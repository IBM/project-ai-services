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
)

// generateKeyAndCSR creates a fresh ECDSA P-256 private key and a matching
// certificate signing request whose CN is the machine hostname.
// The private key is returned as PEM so it can be written to disk alongside
// the signed certificate the gateway sends back.
func generateKeyAndCSR() (keyPEM, csrPEM []byte, err error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(privKey)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	hostname, _ := os.Hostname()
	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   hostname,
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
		filepath.Join(tlsDir, "tls.crt"),
		filepath.Join(tlsDir, "tls.key"),
	)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load mTLS credentials: %w", err)
	}

	return cert, nil
}

// buildTLSConfig returns a *tls.Config for dialling the gateway.
// If ca.crt exists in tlsDir, server verification uses it and ServerName is set
// to workerconstants.GatewayServerName (the SAN embedded in the gateway cert).
// If ca.crt is absent (first bootstrap), InsecureSkipVerify is set as a TOFU
// fallback — it is logged as a warning and only applies to that one dial.
func buildTLSConfig(tlsDir string, clientCert *tls.Certificate) (*tls.Config, error) {
	cfg := &tls.Config{}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}

	caPath := filepath.Join(tlsDir, "ca.crt")
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
		// ServerName must match the SAN in the gateway's server cert.
		cfg.ServerName = workerconstants.GatewayServerName
	case os.IsNotExist(err):
		cfg.InsecureSkipVerify = true //nolint:gosec // intentional TOFU bootstrap fallback
	default:
		return nil, fmt.Errorf("stat ca.crt: %w", err)
	}

	return cfg, nil
}

// writeTLSMaterial creates tlsDir (mode 0700) and writes the three PEM files
// the worker needs for future mTLS dials: tls.crt (cert), tls.key (private key),
// and ca.crt (gateway CA, used for server verification).
func writeTLSMaterial(dir string, certPEM, keyPEM, caCertPEM []byte) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), certPEM, certPerm); err != nil {
		return fmt.Errorf("write tls.crt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), keyPEM, keyPerm); err != nil {
		return fmt.Errorf("write tls.key: %w", err)
	}
	if len(caCertPEM) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "ca.crt"), caCertPEM, certPerm); err != nil {
			return fmt.Errorf("write ca.crt: %w", err)
		}
	}

	return nil
}

// workerNameFromCert reads the CN from the worker's client certificate in tlsDir.
// This recovers the registered worker name on reconnect without any extra state file,
// because the gateway embeds the token-bound worker name as the cert CN at registration time.
func workerNameFromCert(tlsDir string) (string, error) {
	certPEM, err := os.ReadFile(filepath.Join(tlsDir, "tls.crt"))
	if err != nil {
		return "", fmt.Errorf("read tls.crt: %w", err)
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
		filepath.Join(tlsDir, "tls.crt"),
		filepath.Join(tlsDir, "tls.key"),
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
	caPath := filepath.Join(tlsDir, "ca.crt")
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
