package utils

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ValidateCertificateFiles verifies that certificate and key files exist and are readable.
func ValidateCertificateFiles(certPath, keyPath string) error {
	// Validate paths are not empty (fail-fast)
	if certPath == "" {
		return fmt.Errorf("certificate path is empty")
	}
	if keyPath == "" {
		return fmt.Errorf("key path is empty")
	}

	// Validate certificate file
	if err := validateFilePath(certPath, "certificate"); err != nil {
		return err
	}

	// Validate key file
	return validateFilePath(keyPath, "key")
}

// validateFilePath checks if a file exists and is accessible.
func validateFilePath(path, fileType string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file does not exist: %s", fileType, path)
		}
		return fmt.Errorf("cannot access %s file: %w", fileType, err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("%s path is a directory, not a file: %s", fileType, path)
	}

	return nil
}

// LoadCertificate reads and parses a PEM-encoded certificate file.
func LoadCertificate(certPath string) (*x509.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from certificate")
	}

	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("PEM block is not a certificate (type: %s)", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// ValidateCertificateKeyPair verifies that a certificate and private key match.
func ValidateCertificateKeyPair(certPath, keyPath string) error {
	// Load the certificate and key pair
	_, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load certificate with given key: %w", err)
	}

	return nil
}

// ValidateWildcardCertificate checks if a certificate contains a wildcard SAN entry.
func ValidateWildcardCertificate(certPath string) error {
	cert, err := LoadCertificate(certPath)
	if err != nil {
		return err
	}

	// Check Subject Alternative Names (SANs)
	hasWildcard := false
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, "*.") {
			hasWildcard = true

			break
		}
	}

	if !hasWildcard {
		return fmt.Errorf("certificate does not contain a wildcard SAN entry (e.g., *.example.com)")
	}

	return nil
}

// ExtractDomainFromCertificate extracts the base domain from a certificate.
// For wildcard certificates (*.example.com), it returns the base domain (example.com).
// For regular certificates, it returns the first DNS name or Common Name.
func ExtractDomainFromCertificate(certPath string) (string, error) {
	cert, err := LoadCertificate(certPath)
	if err != nil {
		return "", err
	}

	// First, check Subject Alternative Names (SANs) for wildcard domains
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, "*.") {
			// Extract base domain from wildcard (*.example.com → example.com)
			domain := strings.TrimPrefix(san, "*.")
			if domain != "" {
				return domain, nil
			}
		}
	}

	// If no wildcard found, check for regular DNS names in SANs
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0], nil
	}

	// Fall back to Common Name if no SANs
	if cert.Subject.CommonName != "" {
		// Handle wildcard in CN as well
		if strings.HasPrefix(cert.Subject.CommonName, "*.") {
			domain := strings.TrimPrefix(cert.Subject.CommonName, "*.")
			if domain != "" {
				return domain, nil
			}
		}

		return cert.Subject.CommonName, nil
	}

	return "", fmt.Errorf("no domain found in certificate (no SANs or Common Name)")
}

// LoadUserCertificates validates staged certificate files on the host and updates Caddy to load them from container-visible paths.
func LoadUserCertificates(hostCertPath, hostKeyPath, caddyCertPath, caddyKeyPath, adminURL string) error {
	// Read and parse staged host-side certificate files
	_, keyBytes, cert, err := readAndParseCertificates(hostCertPath, hostKeyPath)
	if err != nil {
		return err
	}

	// Validate certificate
	if err := validateCertificateForLoading(cert, keyBytes); err != nil {
		return err
	}

	// Load into Caddy using container-visible mounted file paths
	if err := loadCertificatesIntoCaddy(caddyCertPath, caddyKeyPath, adminURL); err != nil {
		return err
	}

	return nil
}

// readAndParseCertificates reads and parses certificate and key files.
func readAndParseCertificates(certPath, keyPath string) ([]byte, []byte, *x509.Certificate, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read private key: %w", err)
	}

	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil {
		return nil, nil, nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return certBytes, keyBytes, cert, nil
}

// validateCertificateForLoading validates certificate for loading into Caddy.
func validateCertificateForLoading(cert *x509.Certificate, keyBytes []byte) error {
	if err := checkWildcardSAN(cert); err != nil {
		return err
	}

	if err := checkCertificateExpiry(cert); err != nil {
		return err
	}

	return verifyKeyPairMatch(cert, keyBytes)
}

// checkWildcardSAN verifies certificate has wildcard SAN entry.
func checkWildcardSAN(cert *x509.Certificate) error {
	for _, dnsName := range cert.DNSNames {
		if strings.HasPrefix(dnsName, "*.") {
			return nil
		}
	}

	return fmt.Errorf("certificate must contain wildcard SAN entry (e.g., *.example.com)")
}

// checkCertificateExpiry validates certificate is not expired and warns if expiring soon.
func checkCertificateExpiry(cert *x509.Certificate) error {
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not yet valid (valid from: %s)", cert.NotBefore)
	}

	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate has expired (expired on: %s)", cert.NotAfter)
	}

	// Warn if certificate expires soon
	const (
		hoursPerDay       = 24
		expiryWarningDays = 30
	)
	daysUntilExpiry := cert.NotAfter.Sub(now).Hours() / hoursPerDay
	if daysUntilExpiry < expiryWarningDays {
		logger.Infof("Warning: Certificate expires in %.0f days (%s)\n", daysUntilExpiry, cert.NotAfter)
	}

	return nil
}

// verifyKeyPairMatch verifies private key matches certificate public key.
func verifyKeyPairMatch(cert *x509.Certificate, keyBytes []byte) error {
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode private key PEM")
	}

	privateKey, err := parsePrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}

	return matchPublicPrivateKeys(cert.PublicKey, privateKey)
}

// parsePrivateKey parses private key in PKCS8 or PKCS1 format.
func parsePrivateKey(keyData []byte) (interface{}, error) {
	privateKey, err := x509.ParsePKCS8PrivateKey(keyData)
	if err != nil {
		// Try PKCS1 format
		privateKey, err = x509.ParsePKCS1PrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
	}

	return privateKey, nil
}

// matchPublicPrivateKeys verifies public and private keys match.
func matchPublicPrivateKeys(publicKey, privateKey interface{}) error {
	switch pub := publicKey.(type) {
	case *rsa.PublicKey:
		priv, ok := privateKey.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("private key type does not match certificate public key type")
		}
		if pub.N.Cmp(priv.N) != 0 {
			return fmt.Errorf("private key does not match certificate")
		}
	case *ecdsa.PublicKey:
		priv, ok := privateKey.(*ecdsa.PrivateKey)
		if !ok {
			return fmt.Errorf("private key type does not match certificate public key type")
		}
		if pub.X.Cmp(priv.X) != 0 || pub.Y.Cmp(priv.Y) != 0 {
			return fmt.Errorf("private key does not match certificate")
		}
	default:
		return fmt.Errorf("unsupported public key type")
	}

	return nil
}

// loadCertificatesIntoCaddy updates the live Caddy config to load mounted certificate files.
func loadCertificatesIntoCaddy(certPath, keyPath, adminURL string) error {
	payload := map[string]any{
		"certificates": map[string]any{
			"load_files": []map[string]string{
				{
					"certificate": filepath.ToSlash(certPath),
					"key":         filepath.ToSlash(keyPath),
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, adminURL+"/config/apps/tls", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to load certificates: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Infof("Warning: failed to close response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Made with Bob
