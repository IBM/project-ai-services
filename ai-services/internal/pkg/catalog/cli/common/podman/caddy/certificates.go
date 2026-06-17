package caddy

import (
	"fmt"
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
	logger.Debugln("loading ssl certificate to caddy...")
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

// Made with Bob
