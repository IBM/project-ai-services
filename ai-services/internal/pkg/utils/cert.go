package utils

import (
	"crypto"
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
// This function assumes the certificate has already been validated (including wildcard check)
// by ValidateWildcardCertificate before calling this function.
func ExtractDomainFromCertificate(certPath string) (string, error) {
	cert, err := LoadCertificate(certPath)
	if err != nil {
		return "", err
	}

	// Check Subject Alternative Names (SANs) for wildcard domains
	// Certificate is pre-validated, so we know a wildcard exists
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, wildcardPrefix) {
			// Extract base domain from wildcard (*.example.com → example.com)
			domain := strings.TrimPrefix(san, wildcardPrefix)
			if domain != "" {
				return domain, nil
			}
		}
	}

	// Check Common Name for wildcard as fallback
	if cert.Subject.CommonName != "" && strings.HasPrefix(cert.Subject.CommonName, wildcardPrefix) {
		domain := strings.TrimPrefix(cert.Subject.CommonName, wildcardPrefix)
		if domain != "" {
			return domain, nil
		}
	}

	// This should not happen if certificate was properly validated
	return "", fmt.Errorf("failed to extract domain from certificate")
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

// parsePrivateKey parses private key in multiple formats.
// Tries PKCS8 first (universal format supporting RSA, ECDSA, Ed25519),
// then falls back to format-specific parsers (SEC1 for EC, PKCS1 for RSA).
func parsePrivateKey(keyData []byte) (interface{}, error) {
	// Try PKCS8 first - supports all modern key types (RSA, ECDSA, Ed25519)
	privateKey, err := x509.ParsePKCS8PrivateKey(keyData)
	if err == nil {
		return privateKey, nil
	}

	// Try SEC1 format for EC keys (generated by openssl ecparam)
	ecKey, ecErr := x509.ParseECPrivateKey(keyData)
	if ecErr == nil {
		return ecKey, nil
	}

	// Try PKCS1 format for RSA keys (legacy format)
	rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(keyData)
	if rsaErr == nil {
		return rsaKey, nil
	}

	// Return error listing supported formats
	return nil, fmt.Errorf("failed to parse private key: supported formats are PKCS#8 (RSA, ECDSA, Ed25519), SEC1 (EC), and PKCS#1 (RSA)")
}

// matchPublicPrivateKeys verifies public and private keys match using the crypto.Signer interface.
// This generic approach works with all key types (RSA, ECDSA, Ed25519, etc.) without
// requiring type-specific logic.
func matchPublicPrivateKeys(publicKey, privateKey interface{}) error {
	// Verify the private key implements crypto.Signer interface
	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return fmt.Errorf("private key does not implement crypto.Signer interface")
	}

	// Get the public key from the private key
	signerPubKey := signer.Public()

	// Define interface for types that support Equal method
	type equalable interface {
		Equal(crypto.PublicKey) bool
	}

	// Compare the public keys using the Equal method
	if eq, ok := signerPubKey.(equalable); ok {
		if !eq.Equal(publicKey) {
			return fmt.Errorf("private key does not match certificate public key")
		}

		return nil
	}

	return fmt.Errorf("unable to compare public keys: public key type does not support Equal method")
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
