package podman

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
)

func ResetPodmanAuth(ctx context.Context) error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Validate catalog service and confirm reset action
	shouldProceed, err := validateCatalogServiceAndConfirmReset(ctx, deployCtx.Runtime, "podman auth")
	if err != nil {
		return err
	}

	if !shouldProceed {
		return nil
	}

	// Delete podman auth secret.
	logger.InfofCtx(ctx, "Deleting catalog podman auth secret %s", constants.PodmanAuthSecret)
	err = deployCtx.Runtime.DeleteSecret(ctx, constants.PodmanAuthSecret)
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog podman auth secret: %w", err)
	}

	catalogPodLabel := constants.PodComponentKey + "=" + catalogConstants.CatalogComponentValue
	opts, podID, err := catalogUtils.GetCatalogPodConfig(ctx, deployCtx.Runtime, catalogPodLabel)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	logger.InfofCtx(ctx, "Deleting existing catalog pod %s", podID)
	err = deployCtx.Runtime.DeletePod(ctx, podID, utils.BoolPtr(true))
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	_, err = executeCatalogDeployment(ctx, deployCtx, *opts, "")
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	// Reset the local worker pod if one is co-located on this machine.
	if err := resetLocalWorkerIfPresent(ctx, deployCtx.Runtime); err != nil {
		return err
	}

	return nil
}

// resetLocalWorkerIfPresent resets the worker pod's podman auth when a local
// worker pod is detected. If no worker pod exists the call is a no-op.
func resetLocalWorkerIfPresent(ctx context.Context, rt runtime.Runtime) error {
	_, _, err := podmanutils.GetPodConfig(ctx, rt, workerconstants.WorkerPodLabel)
	if err != nil {
		if errors.Is(err, podmanutils.ErrPodNotFound) {
			// No local worker pod — nothing to do.
			return nil
		}

		return fmt.Errorf("failed to check for local worker pod: %w", err)
	}

	logger.InfolnCtx(ctx, "Local worker pod detected — resetting worker podman auth...")

	// Secret was already deleted by the catalog reset above; only the pod needs to be recreated.
	return workerpodman.ResetPodmanAuth(ctx, false)
}
