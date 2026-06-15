package caddy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
func (c *Context) LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	logger.Infoln("loading ssl certificate to caddy...", logger.VerbosityLevelDebug)
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Define staged certificate paths
	stagedCertPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.crt")
	stagedKeyPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.key")

	// Stage certificates
	if err := stageCertificates(baseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to stage certificates for Caddy: %w", err)
	}

	// Get admin URL
	adminURL, err := c.GetHostAdminURL()
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	// Load certificates via Admin API
	if err := utils.LoadUserCertificates(
		stagedCertPath,
		stagedKeyPath,
		filepath.Join(containerDataDir, certsDirName, "tls.crt"),
		filepath.Join(containerDataDir, certsDirName, "tls.key"),
		adminURL,
	); err != nil {
		return fmt.Errorf("failed to load certificates via Admin API: %w", err)
	}

	logger.Infoln("SSL certificates loaded successfully into Caddy")

	return nil
}

// stageCertificates stages SSL certificates for Caddy to use.
func stageCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	caddyDataDir := filepath.Join(baseDir, "common", "caddy")
	certDir := filepath.Join(caddyDataDir, certsDirName)
	if err := os.MkdirAll(certDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create Caddy cert directory: %w", err)
	}

	stagedCertPath := filepath.Join(certDir, "tls.crt")
	stagedKeyPath := filepath.Join(certDir, "tls.key")

	certBytes, err := os.ReadFile(sslCertPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	keyBytes, err := os.ReadFile(sslKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read key file: %w", err)
	}

	if err := os.WriteFile(stagedCertPath, certBytes, filePerm); err != nil {
		return fmt.Errorf("failed to write staged certificate file: %w", err)
	}

	if err := os.WriteFile(stagedKeyPath, keyBytes, filePerm); err != nil {
		return fmt.Errorf("failed to write staged key file: %w", err)
	}

	return nil
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
