package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ResetCatalogCertificate resets the SSL certificates for the catalog service.
// It stages new certificates and loads them into Caddy via the Admin API without pod restart.
// Caddy health is verified internally when connecting to the Admin API.
func ResetCatalogCertificate(ctx context.Context, sslCertPath, sslKeyPath string) error {
	logger.DebuglnCtx(ctx, "Resetting catalog SSL certificates...")

	// Create deployment context to get runtime
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return fmt.Errorf("failed to create deployment context: %w", err)
	}

	// Validate catalog service is running
	isCatalogRunning, err := IsCatalogServiceRunning(ctx, deployCtx.Runtime)
	if err != nil {
		return err
	}

	if !isCatalogRunning {
		return nil
	}

	// Get existing catalog pod details
	opts, err := prepareCatalogOpts(ctx, deployCtx, sslCertPath, sslKeyPath)
	if err != nil {
		return err
	}

	// Get Caddy pod name from templates
	caddyPodName, err := deployCtx.GetCaddyPodName()
	if err != nil {
		return fmt.Errorf("failed to get Caddy pod name: %w", err)
	}

	// Delete the cert secret and Caddy pod to reset the custom certificate.
	// For self-signed certs, the secret may not exist, but execution never reaches here
	// because a domain change is rejected earlier.
	if err := podmanutils.DeleteSecretAndPod(ctx, deployCtx.Runtime, catalogConstant.CatalogCertSecretName, caddyPodName); err != nil {
		return err
	}

	opts.SSLCertPath = sslCertPath
	opts.SSLKeyPath = sslKeyPath
	caddyCtx, err := executeCatalogDeployment(ctx, deployCtx, *opts, "")
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	// Load certificates with health check
	if err := podmanutils.LoadCertificatesToCaddy(ctx, caddyCtx, sslCertPath, sslKeyPath); err != nil {
		return err
	}

	logger.InfolnCtx(ctx, "SSL certificates reset successfully")

	return nil
}

// prepareCatalogOpts fetches the current catalog pod config, validates the base dir,
// and ensures the domain has not changed relative to the new certificates.
func prepareCatalogOpts(ctx context.Context, deployCtx *deploy.DeployContext, sslCertPath, sslKeyPath string) (*catalogUtils.PodmanConfigureOptions, error) {
	opts, _, err := catalogUtils.GetCatalogPodConfig(ctx, deployCtx.Runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	if opts.BaseDir == "" {
		return nil, fmt.Errorf("AI_SERVICES_BASE_DIR not found in catalog configuration")
	}

	if err := utils.ValidateDomainUnchanged(opts.DomainName, sslCertPath, sslKeyPath); err != nil {
		return nil, err
	}

	return opts, nil
}

// Made with Bob
