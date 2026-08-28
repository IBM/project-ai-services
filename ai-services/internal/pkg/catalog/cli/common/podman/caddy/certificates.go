package caddy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	containerCertPath = "/etc/secret/ssl/tls.crt"
	containerkeyPath  = "/etc/secret/ssl/tls.key"
)

// LoadSSLCertificates stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
// Certificate validation is done in the CLI command's PreRunE hook before calling this function.
// Uses timestamped filenames to ensure Caddy loads fresh certificates without requiring a restart.
func (c *Context) LoadSSLCertificates(ctx context.Context, baseDir, sslCertPath, sslKeyPath string) error {
	logger.Debugln("loading ssl certificate to caddy...")
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Get admin URL
	adminURL, err := c.GetHostAdminURL(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	// Load certificates via Admin API using container paths
	if err := utils.LoadUserCertificates(
		sslCertPath,       // host cert path for validation
		sslKeyPath,        // host key path for validation
		containerCertPath, // container cert path
		containerkeyPath,  // container key path
		adminURL,
	); err != nil {
		return fmt.Errorf("failed to load certificates via Admin API: %w", err)
	}

	logger.Infoln("SSL certificates loaded successfully into Caddy")

	return nil
}

// IsCustomCertLoaded checks whether custom SSL certificates are currently loaded in Caddy's live config.
// It queries the Caddy Admin API at /config/apps/tls/certificates and returns true if a load_files entry
// matching the expected container cert and key paths (/etc/secret/ssl/tls.crt and /etc/secret/ssl/tls.key)
// is present. Returns false (without error) when the response is null or no matching entry is found.
func (c *Context) IsCustomCertLoaded(ctx context.Context) (bool, error) {
	adminURL, err := c.GetHostAdminURL(ctx)
	if err != nil {
		return false, err
	}

	result, err := utils.GetCaddyCertificates(ctx, adminURL)
	if err != nil {
		return false, err
	}

	for _, entry := range result.LoadFiles {
		if entry.Certificate == containerCertPath &&
			entry.Key == containerkeyPath {
			return true, nil
		}
	}

	return false, nil
}

// Made with Bob
