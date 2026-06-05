package utils

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	// wildcardPrefix is the prefix used for wildcard domain certificates.
	wildcardPrefix = "*."
	// caddyAPITimeout is the timeout duration for Caddy API requests.
	caddyAPITimeout = 10 * time.Second
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
		if strings.HasPrefix(san, wildcardPrefix) {
			hasWildcard = true

			break
		}
	}

	if !hasWildcard {
		return fmt.Errorf("certificate does not contain a wildcard SAN entry (e.g., %sexample.com)", wildcardPrefix)
	}

	return nil
}

// ExtractDomainFromCertificate extracts the base domain from a wildcard certificate.
// For wildcard certificates (*.example.com), it returns the base domain (example.com).
// This function requires a wildcard certificate to support multiple service subdomains.
func ExtractDomainFromCertificate(certPath string) (string, error) {
	cert, err := LoadCertificate(certPath)
	if err != nil {
		return "", err
	}

	// Check Subject Alternative Names (SANs) for wildcard domains
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, wildcardPrefix) {
			// Extract base domain from wildcard (*.example.com → example.com)
			domain := strings.TrimPrefix(san, wildcardPrefix)
			if domain != "" {
				return domain, nil
			}
		}
	}

	// Check Common Name for wildcard
	if cert.Subject.CommonName != "" && strings.HasPrefix(cert.Subject.CommonName, wildcardPrefix) {
		domain := strings.TrimPrefix(cert.Subject.CommonName, wildcardPrefix)
		if domain != "" {
			return domain, nil
		}
	}

	// Build error message with available domains for debugging
	var availableDomains []string
	availableDomains = append(availableDomains, cert.DNSNames...)
	if cert.Subject.CommonName != "" {
		availableDomains = append(availableDomains, cert.Subject.CommonName)
	}

	if len(availableDomains) > 0 {
		return "", fmt.Errorf("certificate must contain a wildcard domain (%sexample.com) to support multiple service subdomains. Found non-wildcard domains: %v", wildcardPrefix, availableDomains)
	}

	return "", fmt.Errorf("certificate must contain a wildcard domain (%sexample.com) to support multiple service subdomains. No domains found in certificate", wildcardPrefix)
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
		if strings.HasPrefix(dnsName, wildcardPrefix) {
			return nil
		}
	}

	return fmt.Errorf("certificate must contain wildcard SAN entry (e.g., %sexample.com)", wildcardPrefix)
}

// checkCertificateExpiry validates certificate is not expired.
func checkCertificateExpiry(cert *x509.Certificate) error {
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not yet valid (valid from: %s)", cert.NotBefore)
	}

	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate has expired (expired on: %s)", cert.NotAfter)
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

	client := resty.New().SetTimeout(caddyAPITimeout)
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		Patch(adminURL + "/config/apps/tls")

	if err != nil {
		return fmt.Errorf("failed to load certificates: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// Made with Bob
