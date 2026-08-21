package caddy

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	certsDirPath = "/etc/secret/ssl"
)

// LoadSSLCertificates stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
// Certificate validation is done in the CLI command's PreRunE hook before calling this function.
func (c *Context) LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath string) error {
	logger.Debugln("loading ssl certificate to caddy...")
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Get admin URL
	adminURL, err := c.GetHostAdminURL()
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

// Made with Bob
