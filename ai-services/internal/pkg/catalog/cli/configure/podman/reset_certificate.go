package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
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

	if err := deleteSecretAndPod(ctx, deployCtx, caddyPodName, catalogConstant.CatalogCertSecretName); err != nil {
		return err
	}

	opts.SSLCertPath = sslCertPath
	opts.SSLKeyPath = sslKeyPath
	caddyCtx, err := executeCatalogDeployment(context.Background(), deployCtx, *opts, "")
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	// Load certificates with health check
	if err := loadCertificatesToCaddy(ctx, caddyCtx, opts.BaseDir, sslCertPath, sslKeyPath); err != nil {
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

	if err := validateDomainUnchanged(opts, sslCertPath, sslKeyPath); err != nil {
		return nil, err
	}

	return opts, nil
}

// deleteSecretAndPod deletes the caddy cert secret and pod before redeployment.
func deleteSecretAndPod(ctx context.Context, deployCtx *deploy.DeployContext, nameOrID, secretName string) error {
	logger.InfofCtx(context.Background(), "Deleting existing secret %s", secretName)
	if err := deployCtx.Runtime.DeleteSecret(ctx, secretName); err != nil {
		return fmt.Errorf("failed to delete existing catalog secret: %w", err)
	}

	logger.InfofCtx(context.Background(), "Deleting existing pod %s", nameOrID)
	if err := deployCtx.Runtime.DeletePod(ctx, nameOrID, utils.BoolPtr(true)); err != nil {
		return fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	return nil
}

// loadCertificatesToCaddy checks Caddy health and loads SSL certificates.
func loadCertificatesToCaddy(ctx context.Context, caddyCtx *caddy.Context, baseDir, sslCertPath, sslKeyPath string) error {
	// Check Caddy health before attempting to load certificates
	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to create proxy manager: %w", err)
	}

	if err := proxyManager.HealthCheck(ctx); err != nil {
		return fmt.Errorf("caddy health check failed - admin API is not accessible: %w", err)
	}

	// Load new SSL certificates to Caddy
	if err := caddyCtx.LoadSSLCertificates(ctx, baseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to load certificates: %w", err)
	}

	return nil
}

// Made with Bob
