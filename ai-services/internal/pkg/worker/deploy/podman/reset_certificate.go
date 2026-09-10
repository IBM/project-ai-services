package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// ResetWorkerCertificate resets the SSL certificates for the worker service.
// It re-deploys the worker pod with the new certificates and loads them into
// Caddy via the Admin API without restarting the Caddy pod.
func ResetWorkerCertificate(ctx context.Context, sslCertPath, sslKeyPath string) error {
	logger.DebuglnCtx(ctx, "Resetting worker SSL certificates...")
	rt, err := runtime.CreateRuntime(types.RuntimeTypePodman, "")
	if err != nil {
		return fmt.Errorf("failed to init runtime: %w", err)
	}

	opts := workertypes.PodmanWorkerOptions{}

	// Get existing worker pod details and its config.
	podmanOpts, _, err := podmanutils.GetPodConfig(ctx, rt, workerconstants.WorkerPodLabel)
	if err != nil {
		return fmt.Errorf("failed to get existing worker pod details: %w", err)
	}

	// Validate that the domain has not changed relative to the new certificates.
	if err := utils.ValidateDomainUnchanged(podmanOpts.DomainName, sslCertPath, sslKeyPath); err != nil {
		return err
	}

	// Delete certificate secret and worker pod before redeployment.
	if err := podmanutils.DeleteSecretAndPod(ctx, rt, workerconstants.CadyCertSecretName, workerconstants.WorkerCaddyPodName); err != nil {
		return err
	}

	// Re-deploy the worker pod with the new SSL certificate paths.
	opts.Setup = workertypes.Options{
		BaseDir:     podmanOpts.BaseDir,
		DomainName:  podmanOpts.DomainName,
		HTTPSPort:   podmanOpts.HTTPSPort,
		SSLCertPath: sslCertPath,
		SSLKeyPath:  sslKeyPath,
	}

	if err := DeployWorker(ctx, opts); err != nil {
		return fmt.Errorf("failed to deploy worker pod: %w", err)
	}

	// Load the new certificates into the running Caddy instance via the Admin API.
	domainSuffix, err := utils.ComputeDomainSuffix(sslCertPath, sslKeyPath, "")
	if err != nil {
		return err
	}
	caddyCtx := caddy.NewContext(workerconstants.WorkerCaddyPodName, domainSuffix)

	// Load certificates with health check
	if err := podmanutils.LoadCertificatesToCaddy(ctx, caddyCtx, sslCertPath, sslKeyPath); err != nil {
		return err
	}

	logger.InfolnCtx(ctx, "SSL certificates reset successfully")

	return nil
}
