package caddy

import (
	"fmt"
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
func (c *Context) LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	logger.Infoln("loading ssl certificate to caddy...", logger.VerbosityLevelDebug)
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
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
		filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.crt"),
		filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.key"),
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

// Made with Bob
