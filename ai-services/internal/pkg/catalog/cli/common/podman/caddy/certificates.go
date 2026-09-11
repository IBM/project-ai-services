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
func (c *Context) LoadSSLCertificates(ctx context.Context, sslCertPath, sslKeyPath string) error {
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

// LoadCertificatesFromContainerPaths tells Caddy to load the certificates that are
// already mounted inside the container at the well-known paths
// (/etc/secret/ssl/tls.crt and /etc/secret/ssl/tls.key).
// This is used during re-configure when the cert secret was preserved by
// --skip-cleanup and no new host-side cert paths were supplied: the secret is
// already mounted, so only the Caddy Admin API call is needed.
func (c *Context) LoadCertificatesFromContainerPaths(ctx context.Context) error {
	logger.Debugln("loading existing mounted ssl certificates into caddy...")

	adminURL, err := c.GetHostAdminURL(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	if err := utils.LoadCertificatesIntoCaddy(containerCertPath, containerkeyPath, adminURL); err != nil {
		return fmt.Errorf("failed to load mounted certificates via Admin API: %w", err)
	}

	logger.Infoln("Existing SSL certificates loaded successfully into Caddy")

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
