package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	// caTTL is how long a newly generated root CA certificate is valid.
	caTTL = 10 * 365 * 24 * time.Hour // 10 years

	// serverCertTTL is how long a newly generated server certificate is valid.
	serverCertTTL = 365 * 24 * time.Hour // 1 year

	// workerCertTTL is how long a CA-signed worker client certificate is valid.
	workerCertTTL = 365 * 24 * time.Hour // 1 year

	serialBitSize = 128
	// dirPerm lets the Caddy container user (a different uid from the catalog
	// process) reach the files in the PKI directory.
	dirPerm  = 0o755
	certPerm = 0o644
	// keyPerm is the mode of the AES-GCM encrypted server.key, which the Caddy
	// container user must read so the encrypted_file Caddy module can decrypt it in
	// memory. ca.key is never read by Caddy and keeps caKeyPerm.
	keyPerm = 0o644
	// caKeyPerm is the mode of the encrypted CA signing key. Only the catalog reads it.
	caKeyPerm = 0o600
)

// pkiResult groups the four pieces of PKI material the gateway needs.
type pkiResult struct {
	caCert     *x509.Certificate
	caKey      *ecdsa.PrivateKey
	serverCert tls.Certificate
	caCertPool *x509.CertPool
}

// loadOrGeneratePKI loads the four PKI files from pkiDir when they all exist,
// or generates a new ECDSA P-256 root CA and server certificate on first start.
// Either way, the directory and server.key are then made readable for Caddy.
func loadOrGeneratePKI(ctx context.Context, pkiDir string, runtimeType types.RuntimeType) (pkiResult, error) {
	caKeyPath := filepath.Join(pkiDir, "ca.key")
	caCrtPath := filepath.Join(pkiDir, "ca.crt")
	srvKeyPath := filepath.Join(pkiDir, "server.key")
	srvCrtPath := filepath.Join(pkiDir, "server.crt")

	var (
		res pkiResult
		err error
	)

	if fileExists(caKeyPath) && fileExists(caCrtPath) && fileExists(srvKeyPath) && fileExists(srvCrtPath) {
		logger.InfofCtx(ctx, "worker gateway: PKI files found in %s, loading existing material", pkiDir)

		res, err = loadPKI(caCrtPath, caKeyPath, srvCrtPath, srvKeyPath)
	} else {
		logger.InfofCtx(ctx, "worker gateway: PKI directory empty or incomplete — generating new CA and server certificate in %s", pkiDir)

		res, err = generateAndPersistPKI(ctx, pkiDir, runtimeType)
	}
	if err != nil {
		return pkiResult{}, err
	}

	ensureCaddyReadable(ctx, pkiDir)

	return res, nil
}

// generateCA creates a new ECDSA P-256 root CA key and certificate.
func generateCA() (*ecdsa.PrivateKey, *x509.Certificate, []byte, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate CA key: %w", err)
	}

	caSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBitSize))
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "catalog-worker-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(caTTL),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sign CA cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}

	return caKey, caCert, caCertDER, nil
}

// generateServerCert creates a new ECDSA P-256 server key and signs it with the CA.
// dnsNames is the list of DNS SANs embedded in the cert — must exactly match the
// hostname(s) workers will use to dial the gateway.
func generateServerCert(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, dnsNames []string) (*ecdsa.PrivateKey, []byte, error) {
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate server key: %w", err)
	}
	srvSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBitSize))
	srvTemplate := &x509.Certificate{
		SerialNumber: srvSerial,
		Subject:      pkix.Name{CommonName: "Catalog"},
		DNSNames:     dnsNames,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(serverCertTTL),
		// ServerAuth: served by the gateway and by Caddy's private ingress (:9443).
		// ClientAuth: the control-plane Caddy presents this same cert when it dials
		//             a worker's private ingress; Go TLS rejects a client cert
		//             that lacks ExtKeyUsageClientAuth.
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}
	srvCertDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sign server cert: %w", err)
	}

	return srvKey, srvCertDER, nil
}

// writePKIFiles encrypts the two private key PEMs and writes all four PKI files to pkiDir.
func writePKIFiles(pkiDir string, caKeyPEM, caCertDER, srvKeyPEM, srvCertDER []byte, secret string) error {
	caKeyEnc, err := catalogutils.Encrypt(string(caKeyPEM), secret)
	if err != nil {
		return fmt.Errorf("encrypt ca.key: %w", err)
	}

	srvKeyEnc, err := catalogutils.Encrypt(string(srvKeyPEM), secret)
	if err != nil {
		return fmt.Errorf("encrypt server.key: %w", err)
	}

	files := []struct {
		path string
		perm os.FileMode
		data []byte
	}{
		{filepath.Join(pkiDir, "ca.key"), caKeyPerm, []byte(caKeyEnc)},
		{filepath.Join(pkiDir, "ca.crt"), certPerm, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})},
		{filepath.Join(pkiDir, "server.key"), keyPerm, []byte(srvKeyEnc)},
		{filepath.Join(pkiDir, "server.crt"), certPerm, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER})},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, f.data, f.perm); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}

	return nil
}

// ensureCaddyReadable makes the PKI directory and the encrypted server.key readable
// by the Caddy container user. os.WriteFile and os.MkdirAll only apply a mode when
// they create something, so a catalog installed before this change still has a 0700
// directory and a 0600 server.key; this runs on every start to fix them.
// It is best effort: a failure is logged but must not stop the gateway from starting
// (it only affects Caddy's private ingress). ca.key is deliberately left at 0600.
func ensureCaddyReadable(ctx context.Context, pkiDir string) {
	if err := os.Chmod(pkiDir, dirPerm); err != nil { //nolint:gosec // Caddy runs as a different uid and must traverse the directory
		logger.WarningfCtx(ctx, "worker gateway: cannot make %s readable for Caddy: %v", pkiDir, err)
	}

	keyPath := filepath.Join(pkiDir, "server.key")
	if err := os.Chmod(keyPath, keyPerm); err != nil { //nolint:gosec // the file is AES-GCM encrypted; Caddy decrypts it in memory
		logger.WarningfCtx(ctx, "worker gateway: cannot make %s readable for Caddy: %v", keyPath, err)
	}
}

// generateAndPersistPKI creates a new ECDSA P-256 root CA and signs a server
// certificate, then writes all four PEM files to pkiDir.
//
// For OpenShift the server cert's DNS SANs include both the live passthrough
// route host and the internal service endpoint. For Podman the SAN includes the
// domain-derived gateway name.
func serverCertDNSNames(ctx context.Context, runtimeType types.RuntimeType) ([]string, error) {
	switch runtimeType {
	case types.RuntimeTypeOpenShift:
		route, err := GatewayRouteHost(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve gateway route host for cert SAN: %w", err)
		}

		return []string{route, workerconstants.OpenShiftGatewayServiceEndpoint}, nil
	case types.RuntimeTypePodman:
		domainSuffix := utils.GetEnv(proxy.DomainSuffixEnvVar, "")
		if domainSuffix == "" {
			return nil, fmt.Errorf("%s environment variable not set — cannot generate gateway server cert", proxy.DomainSuffixEnvVar)
		}

		// domainSuffix itself is included because a worker's Caddy dials the
		// control-plane private ingress at the bare domain.
		return []string{
			workerconstants.WorkerGatewayName + "." + domainSuffix,
			workerconstants.PodmanGatewayPodName,
			domainSuffix,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime type %q for gateway PKI generation", runtimeType)
	}
}

func generateAndPersistPKI(ctx context.Context, pkiDir string, runtimeType types.RuntimeType) (pkiResult, error) {
	empty := pkiResult{}

	dnsNames, err := serverCertDNSNames(ctx, runtimeType)
	if err != nil {
		return empty, err
	}

	logger.InfofCtx(ctx, "worker gateway: generating server cert with SANs: %v", dnsNames)

	secret := os.Getenv(workerconstants.MTLSEncryptionKeyEnv)
	if secret == "" {
		return empty, fmt.Errorf("worker gateway: PKI encryption key: %s is not set", workerconstants.MTLSEncryptionKeyEnv)
	}

	if err := os.MkdirAll(pkiDir, dirPerm); err != nil {
		return empty, fmt.Errorf("mkdir %s: %w", pkiDir, err)
	}

	caKey, caCert, caCertDER, err := generateCA()
	if err != nil {
		return empty, err
	}

	srvKey, srvCertDER, err := generateServerCert(caCert, caKey, dnsNames)
	if err != nil {
		return empty, err
	}

	caKeyDER, _ := x509.MarshalECPrivateKey(caKey)
	srvKeyDER, _ := x509.MarshalECPrivateKey(srvKey)

	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	srvKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: srvKeyDER})

	if err := writePKIFiles(pkiDir, caKeyPEM, caCertDER, srvKeyPEM, srvCertDER, secret); err != nil {
		return empty, err
	}

	logger.InfofCtx(ctx, "worker gateway: PKI generated and persisted to %s", pkiDir)

	serverCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER}),
		srvKeyPEM,
	)
	if err != nil {
		return empty, fmt.Errorf("build server tls.Certificate: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return pkiResult{caCert: caCert, caKey: caKey, serverCert: serverCert, caCertPool: pool}, nil
}

// loadCAMaterial reads and decrypts ca.crt + ca.key, returning the parsed certificate and key.
func loadCAMaterial(caCrtPath, caKeyPath, secret string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	caCertPEM, err := os.ReadFile(caCrtPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", caCrtPath, err)
	}
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("decode %s: not valid PEM", caCrtPath)
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", caCrtPath, err)
	}

	caKeyEnc, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", caKeyPath, err)
	}
	caKeyPEMStr, err := catalogutils.Decrypt(string(caKeyEnc), secret)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt %s: %w", caKeyPath, err)
	}
	keyBlock, _ := pem.Decode([]byte(caKeyPEMStr))
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("decode %s: not valid PEM after decryption", caKeyPath)
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", caKeyPath, err)
	}

	return caCert, caKey, nil
}

// loadServerKeyPEM reads and decrypts server.key, returning the plaintext PEM bytes.
func loadServerKeyPEM(srvKeyPath, secret string) ([]byte, error) {
	srvKeyEnc, err := os.ReadFile(srvKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", srvKeyPath, err)
	}
	srvKeyPEMStr, err := catalogutils.Decrypt(string(srvKeyEnc), secret)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", srvKeyPath, err)
	}

	return []byte(srvKeyPEMStr), nil
}

// loadPKI reads all four PKI files from disk and returns the parsed material.
// ca.key and server.key are decrypted using the MTLS_ENCRYPTION_KEY env var
// before being parsed.
func loadPKI(caCrtPath, caKeyPath, srvCrtPath, srvKeyPath string) (pkiResult, error) {
	empty := pkiResult{}

	secret := os.Getenv(workerconstants.MTLSEncryptionKeyEnv)
	if secret == "" {
		return empty, fmt.Errorf("worker gateway: PKI decryption key: %s is not set", workerconstants.MTLSEncryptionKeyEnv)
	}

	caCert, caKey, err := loadCAMaterial(caCrtPath, caKeyPath, secret)
	if err != nil {
		return empty, err
	}

	srvCertPEM, err := os.ReadFile(srvCrtPath)
	if err != nil {
		return empty, fmt.Errorf("read %s: %w", srvCrtPath, err)
	}

	srvKeyPEM, err := loadServerKeyPEM(srvKeyPath, secret)
	if err != nil {
		return empty, err
	}

	serverCert, err := tls.X509KeyPair(srvCertPEM, srvKeyPEM)
	if err != nil {
		return empty, fmt.Errorf("load server key pair: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return pkiResult{caCert: caCert, caKey: caKey, serverCert: serverCert, caCertPool: pool}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
