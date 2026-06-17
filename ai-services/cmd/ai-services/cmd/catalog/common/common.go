package common

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func InitAndValidateRuntimeFlag(runtimeType string) error {
	// Initialize runtime factory based on flag
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Debugf("Using runtime: %s\n", rt)

	// Check if podman runtime is being used on unsupported platform
	if err := utils.CheckPodmanPlatformSupport(rt); err != nil {
		return err
	}

	return validateRuntimeType(rt)
}

func ConfigureRuntimeFlag(cmd *cobra.Command, runtimeType *string) {
	cmd.Flags().StringVarP(runtimeType, constants.RuntimeFlag, "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired(constants.RuntimeFlag)
}

func validateRuntimeType(runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman:
		return nil
	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("catalog cmd is not yet supported for OpenShift runtime")
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}
