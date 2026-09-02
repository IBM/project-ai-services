package utils

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// UninstallOptions carries the parameters needed to uninstall a worker node.
type UninstallOptions struct {
	// RuntimeType is the local runtime (podman / openshift).
	RuntimeType types.RuntimeType

	// AutoYes skips the interactive confirmation prompt.
	AutoYes bool
}

// ConfirmUninstall prompts the user to confirm uninstall and logs pods to be deleted.
func ConfirmUninstall(ctx context.Context, pods []types.Pod, autoYes bool) (bool, error) {
	if autoYes {
		return true, nil
	}

	// Print pods to be deleted
	logger.InfofCtx(ctx, "Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.InfofCtx(ctx, "\t-> %s\n", pod.Name)
	}

	// Confirm Uninstall
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfolnCtx(ctx, "Uninstall cancelled")

		return false, nil
	}

	return true, nil
}
