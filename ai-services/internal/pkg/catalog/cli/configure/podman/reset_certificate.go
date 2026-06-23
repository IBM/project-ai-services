package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ResetCatalogCertificate resets the SSL certificates for the catalog service.
// It stages new certificates and loads them into Caddy via the Admin API without pod restart.
// Caddy health is verified internally when connecting to the Admin API.
func ResetCatalogCertificate(sslCertPath, sslKeyPath string) error {
	logger.DebuglnCtx(context.Background(), "Resetting catalog SSL certificates...")

	// Create deployment context to get runtime
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return fmt.Errorf("failed to create deployment context: %w", err)
	}

	// Validate catalog service is running
	isCatalogRunning, err := IsCatalogServiceRunning(deployCtx.Runtime)
	if err != nil {
		return err
	}

	if !isCatalogRunning {
		return nil
	}

	// Get existing catalog pod details
	opts, _, err := catalogUtils.GetCatalogPodConfig(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	if opts.BaseDir == "" {
		return fmt.Errorf("AI_SERVICES_BASE_DIR not found in catalog configuration")
	}

	// Validate that domain hasn't changed
	if err := validateDomainUnchanged(opts, sslCertPath, sslKeyPath); err != nil {
		return err
	}

	// Get Caddy pod name from templates
	caddyPodName, err := deployCtx.GetCaddyPodName()
	if err != nil {
		return fmt.Errorf("failed to get Caddy pod name: %w", err)
	}

	// Create Caddy context for certificate operations
	caddyCtx := caddy.NewContext(caddyPodName, "")

	// Load certificates with health check
	if err := loadCertificatesToCaddy(caddyCtx, opts.BaseDir, sslCertPath, sslKeyPath); err != nil {
		return err
	}

	logger.InfolnCtx(context.Background(), "SSL certificates reset successfully")

	return nil
}

// loadCertificatesToCaddy checks Caddy health and loads SSL certificates.
func loadCertificatesToCaddy(caddyCtx *caddy.Context, baseDir, sslCertPath, sslKeyPath string) error {
	// Check Caddy health before attempting to load certificates
	proxyManager, err := caddyCtx.CreateProxyManager()
	if err != nil {
		return fmt.Errorf("failed to create proxy manager: %w", err)
	}

	if err := proxyManager.HealthCheck(); err != nil {
		return fmt.Errorf("caddy health check failed - admin API is not accessible: %w", err)
	}

	// Load new SSL certificates to Caddy
	if err := caddyCtx.LoadSSLCertificates(baseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to load certificates: %w", err)
	}

	return nil
}

// Made with Bob
