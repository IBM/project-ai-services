package bootstrap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	serialBits      = 128
	certValidHours  = 24 * time.Hour
)

// SelfSignedCert holds the file paths for a generated self-signed certificate pair.
type SelfSignedCert struct {
	CertPath string
	KeyPath  string
}

// GenerateSelfSignedWildcardCert generates a self-signed wildcard cert for domain (e.g. "example.com" → SAN "*.example.com") and writes PEM files to dir.
func GenerateSelfSignedWildcardCert(dir string, domain string) (SelfSignedCert, error) {
	certDER, priv, err := generateCertDER(domain)
	if err != nil {
		return SelfSignedCert{}, err
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return SelfSignedCert{}, fmt.Errorf("failed to marshal private key: %w", err)
	}

	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	if err = writePEMFile(certPath, "CERTIFICATE", certDER); err != nil {
		return SelfSignedCert{}, err
	}

	if err = writePEMFile(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		return SelfSignedCert{}, err
	}

	return SelfSignedCert{CertPath: certPath, KeyPath: keyPath}, nil
}

// generateCertDER generates a self-signed wildcard certificate template and returns the DER-encoded cert and private key.
func generateCertDER(domain string) ([]byte, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	wildcardDomain := "*." + domain
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   wildcardDomain,
			Organization: []string{"AI Services E2E Test"},
		},
		DNSNames:  []string{wildcardDomain, domain},
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(certValidHours),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	return certDER, priv, nil
}

// writePEMFile creates a file at path and writes a single PEM block of the given type and DER bytes.
func writePEMFile(path, pemType string, der []byte) (err error) {
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close file %s: %w", path, cerr)
		}
	}()

	if err = pem.Encode(f, &pem.Block{Type: pemType, Bytes: der}); err != nil {
		return fmt.Errorf("failed to write PEM to %s: %w", path, err)
	}

	return nil
}

// WriteInvalidCertFiles writes non-PEM cert and key files to dir; used for negative certificate-validation tests.
func WriteInvalidCertFiles(dir string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, "invalid.crt")
	keyPath = filepath.Join(dir, "invalid.key")

	if werr := os.WriteFile(certPath, []byte("this is not a valid PEM certificate"), dirPerm); werr != nil {
		return "", "", fmt.Errorf("failed to write invalid cert: %w", werr)
	}

	if werr := os.WriteFile(keyPath, []byte("this is not a valid PEM key"), dirPerm); werr != nil {
		return "", "", fmt.Errorf("failed to write invalid key: %w", werr)
	}

	return certPath, keyPath, nil
}
