package podman

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ResetCatalogCertificate resets the SSL certificates for the catalog service.
// It stages new certificates and loads them into Caddy via the Admin API without pod restart.
func ResetCatalogCertificate(sslCertPath, sslKeyPath string) error {
	logger.Infoln("resetting catalog SSL certificates...", logger.VerbosityLevelDebug)

	// Create deployment context to get runtime
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return fmt.Errorf("failed to create deployment context: %w", err)
	}

	// Get existing catalog pod details
	opts, _, err := getCatalogPodDetails(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	if opts.BaseDir == "" {
		return fmt.Errorf("AI_SERVICES_BASE_DIR not found in catalog configuration")
	}

	// Validate certificate files exist and are readable
	if err := utils.ValidateCertificateFiles(sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("certificate validation failed: %w", err)
	}

	// Validate certificate and key match
	if err := utils.ValidateCertificateKeyPair(sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("certificate and key validation failed: %w", err)
	}

	// Get pod name - construct it from the app name
	podName := catalogConstant.CatalogAppName

	// Create Caddy context
	caddyCtx := caddy.NewContext(podName, "")

	// Use the LoadSSLCertificates method which handles both staging and loading
	logger.Infoln("loading SSL certificates to Caddy...", logger.VerbosityLevelDebug)
	if err := caddyCtx.LoadSSLCertificates(opts.BaseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to load certificates: %w", err)
	}

	logger.Infof("SSL certificates reset successfully")

	return nil
}

// Made with Bob
