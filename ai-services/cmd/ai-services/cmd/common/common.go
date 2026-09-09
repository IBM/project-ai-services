// Package common provides CLI helpers shared across all top-level commands
// (catalog, worker, bootstrap, etc.).
package common

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// InitAndValidateRuntimeFlag validates the runtime flag value, initialises
// vars.RuntimeFactory, and checks platform support. It must be called in
// PreRunE before any code that reads vars.RuntimeFactory.
func InitAndValidateRuntimeFlag(runtimeType string) error {
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Debugf("Using runtime: %s\n", rt)

	if err := utils.CheckPodmanPlatformSupport(rt); err != nil {
		return err
	}

	return validateRuntimeType(rt)
}

// ConfigureRuntimeFlag registers the --runtime / -r flag on cmd and marks it
// required. Use this in every command that accepts a runtime type.
func ConfigureRuntimeFlag(cmd *cobra.Command, runtimeType *string) {
	cmd.Flags().StringVarP(runtimeType, constants.RuntimeFlag, "r", "",
		fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired(constants.RuntimeFlag)
}

func validateRuntimeType(runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman, types.RuntimeTypeOpenShift:
		return nil
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// IsCatalogLocalWorker inspects the named catalog pod and returns true when
// the LOCAL_WORKER environment variable is set to "true" inside it.
// If the catalog pod does not exist on this node, it returns false, nil —
// which is the expected state on a standalone remote worker.
func IsCatalogLocalWorker(ctx context.Context, rt runtime.Runtime, podName string) (bool, error) {
	pod, err := rt.InspectPod(ctx, podName)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspect catalog pod: %w", err)
	}

	for _, container := range pod.Containers {
		cInfo, err := rt.InspectContainer(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("inspect container %s failed: %w", container.ID, err)
		}
		if cInfo.Env[workerconstants.LocalWorkerEnvVar] == "true" {
			return true, nil
		}
	}

	return false, nil
}
