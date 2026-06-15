package caddy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// ErrCertificatesAlreadyLoaded is returned when certificates are identical and don't need reloading.
var ErrCertificatesAlreadyLoaded = errors.New("certificates already loaded")

// LoadSSLCertificates stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
// It validates certificate changes and skips loading if certificates are identical to existing ones.
func (c *Context) LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	logger.Infoln("loading ssl certificate to caddy...", logger.VerbosityLevelDebug)
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Define staged certificate paths
	stagedCertPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.crt")
	stagedKeyPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.key")

	// Validate certificate update (check if certificates changed)
	err := validateCertificateUpdate(sslCertPath, sslKeyPath, stagedCertPath, stagedKeyPath)
	if err != nil {
		if errors.Is(err, ErrCertificatesAlreadyLoaded) {
			// Certificates are identical, skip staging and loading
			logger.Infoln("Certificates unchanged, skipping reload")
			return nil
		}
		// Certificate content changed - block update
		return err
	}

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

// validateCertificateUpdate validates certificate changes during reconfigure.
// Returns an error if validation fails or if certificates should be skipped (with special error).
// Returns nil if certificates can proceed with loading.
func validateCertificateUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath string) error {
	// Check if staged certificates exist from previous successful deployment
	_, certErr := os.Stat(stagedCertPath)
	_, keyErr := os.Stat(stagedKeyPath)

	if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
		// No staged certificates found - either:
		// 1. Previous deployment used auto-generated certs, OR
		// 2. Previous custom cert deployment failed during staging
		// In both cases, allow cert loading since domain is already validated
		logger.Infof("No existing custom certificates found, loading new certificates\n")
		return nil
	}

	// Staged certificates exist - compare content
	needsUpdate, err := certificatesNeedUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath)
	if err != nil {
		return fmt.Errorf("failed to check certificate status: %w", err)
	}

	if !needsUpdate {
		// Certificates are identical - return special error to signal skip
		return ErrCertificatesAlreadyLoaded
	}

	// Certificates differ - block update
	return fmt.Errorf("certificate content change not allowed during reconfigure. Please reset cert")
}

// certificatesNeedUpdate checks if new certificates differ from existing staged certificates.
// Returns true if certificates need to be updated, false if they are identical.
// Assumes staged certificates exist (caller validates this).
func certificatesNeedUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath string) (bool, error) {
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
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Made with Bob
