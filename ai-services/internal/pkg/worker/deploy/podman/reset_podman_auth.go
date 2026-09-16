package podman

import (
	"context"
	"fmt"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// ResetPodmanAuth resets the worker pod's podman auth.
func ResetPodmanAuth(ctx context.Context, deleteSecret bool) error {
	rt, err := runtime.CreateRuntime(types.RuntimeTypePodman, "")
	if err != nil {
		return fmt.Errorf("worker reset: init runtime: %w", err)
	}

	opts := workertypes.PodmanWorkerOptions{}

	// 1. Get the worker pod with label "ai-services.io/component=worker" and extract its config.
	podmanOpts, podID, err := podmanutils.GetPodConfig(ctx, rt, workerconstants.WorkerPodLabel)
	if err != nil {
		return fmt.Errorf("failed to get existing worker pod details: %w", err)
	}

	// 2. Optionally delete the podman auth secret, always delete the worker pod.
	if deleteSecret {
		if err := podmanutils.DeleteSecretAndPod(ctx, rt, constants.PodmanAuthSecret, podID); err != nil {
			return err
		}
	} else {
		if err := podmanutils.DeletePod(ctx, rt, podID); err != nil {
			return err
		}
	}

	// 3. Re-deploy the worker pod with the extracted configuration.
	opts.Setup = workertypes.Options{
		BaseDir:    podmanOpts.BaseDir,
		DomainName: podmanOpts.DomainName,
		HTTPSPort:  podmanOpts.HTTPSPort,
	}
	opts.GatewayAddr = podmanOpts.GatewayAddr

	if err := DeployWorker(ctx, opts); err != nil {
		return fmt.Errorf("failed to deploy worker pod: %w", err)
	}

	return nil
}
