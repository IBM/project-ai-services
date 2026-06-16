package caddy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	certsDirName     = "certs"
	containerDataDir = "/data/caddy"
	dirPerm          = 0o755
	filePerm         = 0o644
)

// LoadSSLCertificates stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
// Certificate validation is done in the CLI command's PreRunE hook before calling this function.
// Uses timestamped filenames to ensure Caddy loads fresh certificates without requiring a restart.
func (c *Context) LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	logger.Infoln("loading ssl certificate to caddy...", logger.VerbosityLevelDebug)
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Stage certificates with timestamped filenames (deletes old certificates)
	certFilename, keyFilename, err := stageCertificates(baseDir, sslCertPath, sslKeyPath)
	if err != nil {
		return fmt.Errorf("failed to stage certificates for Caddy: %w", err)
	}

	// Define staged certificate paths with timestamped filenames
	stagedCertPath := filepath.Join(baseDir, "common", "caddy", certsDirName, certFilename)
	stagedKeyPath := filepath.Join(baseDir, "common", "caddy", certsDirName, keyFilename)

	// Get admin URL
	adminURL, err := c.GetHostAdminURL()
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	// Load certificates via Admin API with timestamped paths
	if err := utils.LoadUserCertificates(
		stagedCertPath,
		stagedKeyPath,
		filepath.Join(containerDataDir, certsDirName, certFilename),
		filepath.Join(containerDataDir, certsDirName, keyFilename),
		adminURL,
	); err != nil {
		return fmt.Errorf("failed to load certificates via Admin API: %w", err)
	}

	logger.Infoln("SSL certificates loaded successfully into Caddy")

	return nil
}

// stageCertificates stages SSL certificates for Caddy to use with timestamped filenames.
// Deletes old certificate files before staging new ones to ensure only one set exists.
// Returns the certificate and key filenames for use in loading.
func stageCertificates(baseDir, sslCertPath, sslKeyPath string) (string, string, error) {
	caddyDataDir := filepath.Join(baseDir, "common", "caddy")
	certDir := filepath.Join(caddyDataDir, certsDirName)
	if err := os.MkdirAll(certDir, dirPerm); err != nil {
		return "", "", fmt.Errorf("failed to create Caddy cert directory: %w", err)
	}

	// Delete old certificate files (tls-*.crt and tls-*.key)
	oldCerts, _ := filepath.Glob(filepath.Join(certDir, "tls-*.crt"))
	for _, oldCert := range oldCerts {
		if err := os.Remove(oldCert); err != nil {
			logger.Warningf("Failed to remove old certificate %s: %v", oldCert, err)
		}
	}
	oldKeys, _ := filepath.Glob(filepath.Join(certDir, "tls-*.key"))
	for _, oldKey := range oldKeys {
		if err := os.Remove(oldKey); err != nil {
			logger.Warningf("Failed to remove old key %s: %v", oldKey, err)
		}
	}

	// Generate timestamped filenames
	timestamp := time.Now().Unix()
	certFilename := fmt.Sprintf("tls-%d.crt", timestamp)
	keyFilename := fmt.Sprintf("tls-%d.key", timestamp)

	stagedCertPath := filepath.Join(certDir, certFilename)
	stagedKeyPath := filepath.Join(certDir, keyFilename)

	// Read certificate and key files
	certBytes, err := os.ReadFile(sslCertPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read certificate file: %w", err)
	}

	keyBytes, err := os.ReadFile(sslKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read key file: %w", err)
	}

	// Write staged certificate and key with timestamped filenames
	if err := os.WriteFile(stagedCertPath, certBytes, filePerm); err != nil {
		return "", "", fmt.Errorf("failed to write staged certificate file: %w", err)
	}

	if err := os.WriteFile(stagedKeyPath, keyBytes, filePerm); err != nil {
		return "", "", fmt.Errorf("failed to write staged key file: %w", err)
	}

	return certFilename, keyFilename, nil
}

// CertificatesNeedUpdate checks if new certificates differ from existing staged certificates.
// Returns true if certificates need to be updated, false if they are identical.
// Assumes staged certificates exist (caller validates this).
func CertificatesNeedUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath string) (bool, error) {
	// Compare certificate hashes
	newCertHash, err := computeFileHash(newCertPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for new certificate: %w", err)
	}

	stagedCertHash, err := computeFileHash(stagedCertPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for staged certificate: %w", err)
	}

	// Compare key hashes
	newKeyHash, err := computeFileHash(newKeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for new key: %w", err)
	}

	stagedKeyHash, err := computeFileHash(stagedKeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for staged key: %w", err)
	}

	// If either cert or key differs, need update
	return newCertHash != stagedCertHash || newKeyHash != stagedKeyHash, nil
}

// computeFileHash computes SHA256 hash of a file.
func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Warningf("Failed to close file %s: %v", filePath, closeErr)
		}
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Made with Bob
