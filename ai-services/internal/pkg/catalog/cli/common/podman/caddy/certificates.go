package caddy

import (
	"context"
	"fmt"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	certsDirPath = "/etc/secret/ssl"
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
		sslCertPath,                             // host cert path for validation
		sslKeyPath,                              // host key path for validation
		fmt.Sprintf("%s/tls.crt", certsDirPath), // container cert path
		fmt.Sprintf("%s/tls.key", certsDirPath), // container key path
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

	type loadFilesEntry struct {
		Certificate string `json:"certificate"`
		Key         string `json:"key"`
	}
	type certResponse struct {
		LoadFiles []loadFilesEntry `json:"load_files"`
	}

	var result certResponse
	resp, err := resty.New().R().
		SetResult(&result).
		Get(adminURL + "/config/apps/tls/certificates")
	if err != nil {
		return false, fmt.Errorf("failed to query Caddy certificates config: %w", err)
	}
	if resp.IsError() {
		return false, fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode(), resp.String())
	}

	for _, entry := range result.LoadFiles {
		if entry.Certificate == fmt.Sprintf("%s/tls.crt", certsDirPath) &&
			entry.Key == fmt.Sprintf("%s/tls.key", certsDirPath) {
			return true, nil
		}
	}

	return false, nil
}

// Made with Bob
