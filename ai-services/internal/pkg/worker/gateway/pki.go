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

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
	dirPerm       = 0o700
	certPerm      = 0o644
	keyPerm       = 0o600
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
func loadOrGeneratePKI(ctx context.Context, pkiDir string, runtimeType types.RuntimeType) (pkiResult, error) {
	caKeyPath := filepath.Join(pkiDir, "ca.key")
	caCrtPath := filepath.Join(pkiDir, "ca.crt")
	srvKeyPath := filepath.Join(pkiDir, "server.key")
	srvCrtPath := filepath.Join(pkiDir, "server.crt")

	if fileExists(caKeyPath) && fileExists(caCrtPath) && fileExists(srvKeyPath) && fileExists(srvCrtPath) {
		logger.InfofCtx(ctx, "worker gateway: PKI files found in %s, loading existing material", pkiDir)

		return loadPKI(caCrtPath, caKeyPath, srvCrtPath, srvKeyPath)
	}

	logger.InfofCtx(ctx, "worker gateway: PKI directory empty or incomplete — generating new CA and server certificate in %s", pkiDir)

	return generateAndPersistPKI(ctx, pkiDir, runtimeType)
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
// serverName is the hostname embedded as the DNS SAN (must match what workers dial).
func generateServerCert(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, serverName string) (*ecdsa.PrivateKey, []byte, error) {
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate server key: %w", err)
	}
	srvSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBitSize))
	srvTemplate := &x509.Certificate{
		SerialNumber: srvSerial,
		Subject:      pkix.Name{CommonName: "Catalog"},
		DNSNames:     []string{serverName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(serverCertTTL),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	srvCertDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sign server cert: %w", err)
	}

	return srvKey, srvCertDER, nil
}

// generateAndPersistPKI creates a new ECDSA P-256 root CA and signs a server
// certificate, then writes all four PEM files to pkiDir.
//
// For OpenShift the server cert's DNS SAN is populated by fetching the live
// passthrough route host via GatewayRouteHost rather than using a hardcoded
// hostname. For all other runtimes the static internal name is used.
func generateAndPersistPKI(ctx context.Context, pkiDir string, runtimeType types.RuntimeType) (pkiResult, error) {
	empty := pkiResult{}

	serverName := workerconstants.GatewayServerName
	if runtimeType == types.RuntimeTypeOpenShift {
		host, err := GatewayRouteHost(ctx)
		if err != nil {
			return empty, fmt.Errorf("resolve gateway route host for cert SAN: %w", err)
		}
		serverName = host
	}

	if err := os.MkdirAll(pkiDir, dirPerm); err != nil {
		return empty, fmt.Errorf("mkdir %s: %w", pkiDir, err)
	}

	caKey, caCert, caCertDER, err := generateCA()
	if err != nil {
		return empty, err
	}

	srvKey, srvCertDER, err := generateServerCert(caCert, caKey, serverName)
	if err != nil {
		return empty, err
	}

	caKeyDER, _ := x509.MarshalECPrivateKey(caKey)
	srvKeyDER, _ := x509.MarshalECPrivateKey(srvKey)

	files := []struct {
		path string
		perm os.FileMode
		data []byte
	}{
		{filepath.Join(pkiDir, "ca.key"), keyPerm, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})},
		{filepath.Join(pkiDir, "ca.crt"), certPerm, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})},
		{filepath.Join(pkiDir, "server.key"), keyPerm, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: srvKeyDER})},
		{filepath.Join(pkiDir, "server.crt"), certPerm, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER})},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, f.data, f.perm); err != nil {
			return empty, fmt.Errorf("write %s: %w", f.path, err)
		}
	}
	logger.InfofCtx(ctx, "worker gateway: PKI generated and persisted to %s", pkiDir)

	serverCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: srvKeyDER}),
	)
	if err != nil {
		return empty, fmt.Errorf("build server tls.Certificate: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return pkiResult{caCert: caCert, caKey: caKey, serverCert: serverCert, caCertPool: pool}, nil
}

// loadPKI reads all four PKI files from disk and returns the parsed material.
func loadPKI(caCrtPath, caKeyPath, srvCrtPath, srvKeyPath string) (pkiResult, error) {
	empty := pkiResult{}

	caCertPEM, err := os.ReadFile(caCrtPath)
	if err != nil {
		return empty, fmt.Errorf("read %s: %w", caCrtPath, err)
	}
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return empty, fmt.Errorf("decode %s: not valid PEM", caCrtPath)
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return empty, fmt.Errorf("parse %s: %w", caCrtPath, err)
	}

	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return empty, fmt.Errorf("read %s: %w", caKeyPath, err)
	}
	keyBlock, _ := pem.Decode(caKeyPEM)
	if keyBlock == nil {
		return empty, fmt.Errorf("decode %s: not valid PEM", caKeyPath)
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return empty, fmt.Errorf("parse %s: %w", caKeyPath, err)
	}

	srvCertPEM, err := os.ReadFile(srvCrtPath)
	if err != nil {
		return empty, fmt.Errorf("read %s: %w", srvCrtPath, err)
	}
	srvKeyPEM, err := os.ReadFile(srvKeyPath)
	if err != nil {
		return empty, fmt.Errorf("read %s: %w", srvKeyPath, err)
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
