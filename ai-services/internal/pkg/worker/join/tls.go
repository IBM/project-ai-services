package join

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
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	dirPerm  = 0o700
	certPerm = 0o644
	keyPerm  = 0o600

	tlsCertFile = "tls.crt"
	tlsKeyFile  = "tls.key"
	caCertFile  = "ca.crt"
)

func generateKeyAndCSR() (keyPEM, csrPEM []byte, err error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(privKey)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"system:workers"},
		},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CSR: %w", err)
	}
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	return keyPEM, csrPEM, nil
}

// loadClientCert loads the worker's mTLS key pair from tlsDir.
func loadClientCert(tlsDir string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(tlsDir, tlsCertFile),
		filepath.Join(tlsDir, tlsKeyFile),
	)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load mTLS credentials: %w", err)
	}

	return cert, nil
}

// buildTLSConfig returns a *tls.Config for dialing the gateway. When the target
// includes a host or IP, we verify against that exact address. Otherwise we fall
// back to the podman/internal default name.
func buildTLSConfig(gatewayAddr, tlsDir string, clientCert *tls.Certificate) (*tls.Config, error) {
	cfg := &tls.Config{}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}

	caPath := filepath.Join(tlsDir, caCertFile)
	switch _, err := os.Stat(caPath); {
	case err == nil:
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read ca.crt: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse ca.crt: no valid certificates found")
		}
		cfg.RootCAs = pool
		cfg.ServerName = gatewayServerName(gatewayAddr)
	case os.IsNotExist(err):
		cfg.InsecureSkipVerify = true //nolint:gosec // intentional TOFU bootstrap fallback
	default:
		return nil, fmt.Errorf("stat ca.crt: %w", err)
	}

	return cfg, nil
}

func gatewayServerName(gatewayAddr string) string {
	if gatewayAddr == "" {
		return workerconstants.GatewayServerName
	}
	if parsed, err := url.Parse(gatewayAddr); err == nil && parsed.Host != "" {
		gatewayAddr = parsed.Host
	}
	if host, _, err := net.SplitHostPort(gatewayAddr); err == nil {
		gatewayAddr = host
	}
	if gatewayAddr == "" {
		return workerconstants.GatewayServerName
	}
	if ip := net.ParseIP(gatewayAddr); ip != nil {
		return workerconstants.GatewayServerName
	}

	return gatewayAddr
}

// writeTLSMaterial creates tlsDir (mode 0700) and writes the three PEM files
// the worker needs for future mTLS dials: tls.crt (cert), tls.key (private key),
// and ca.crt (gateway CA, used for server verification).
func writeTLSMaterial(dir string, certPEM, keyPEM, caCertPEM []byte) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, tlsCertFile), certPEM, certPerm); err != nil {
		return fmt.Errorf("write %s: %w", tlsCertFile, err)
	}
	if err := os.WriteFile(filepath.Join(dir, tlsKeyFile), keyPEM, keyPerm); err != nil {
		return fmt.Errorf("write %s: %w", tlsKeyFile, err)
	}
	if len(caCertPEM) > 0 {
		if err := os.WriteFile(filepath.Join(dir, caCertFile), caCertPEM, certPerm); err != nil {
			return fmt.Errorf("write %s: %w", caCertFile, err)
		}
	}

	return nil
}

// workerNameFromCert reads the CN from the worker's client certificate in tlsDir.
// This recovers the registered worker name on reconnect without any extra state file,
// because the gateway embeds the token-bound worker name as the cert CN at registration time.
func workerNameFromCert(tlsDir string) (string, error) {
	certPEM, err := os.ReadFile(filepath.Join(tlsDir, tlsCertFile))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", tlsCertFile, err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("tls.crt: not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse tls.crt: %w", err)
	}
	if cert.Subject.CommonName == "" {
		return "", fmt.Errorf("tls.crt: CN is empty")
	}

	return cert.Subject.CommonName, nil
}

// hasValidTLSCredentials returns true when the on-disk credentials in tlsDir
// are structurally valid and not expired:
//  1. tls.crt + tls.key load without error.
//  2. The certificate has not yet expired.
//  3. If ca.crt is present, the cert verifies against it (catches CA rotation).
func hasValidTLSCredentials(ctx context.Context, tlsDir string) bool {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(tlsDir, tlsCertFile),
		filepath.Join(tlsDir, tlsKeyFile),
	)
	if err != nil {
		return false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false
	}
	if !time.Now().Before(leaf.NotAfter) {
		return false
	}

	// Verify the client cert against the stored CA so we catch cases where
	// the CA was rotated and the on-disk cert is no longer trusted.
	caPath := filepath.Join(tlsDir, caCertFile)
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No CA stored yet — key-pair alone is sufficient evidence.
			return true
		}
		logger.WarningfCtx(ctx, "worker join: credential check: read ca.crt: %v", err)

		return false
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		logger.WarningfCtx(ctx, "worker join: credential check: parse ca.crt failed")

		return false
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		logger.WarningfCtx(ctx, "worker join: credential check: cert not trusted by stored CA: %v", err)

		return false
	}

	return true
}
