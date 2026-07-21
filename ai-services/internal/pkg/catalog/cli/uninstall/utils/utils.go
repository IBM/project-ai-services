package utils

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// UninstallOptions contains the configuration for uninstalling the catalog service.
type UninstallOptions struct {
	Runtime     types.RuntimeType
	AutoYes     bool
	SkipCleanup bool
}

// ConfirmDeletion prompts the user to confirm deletion and logs pods to be deleted.
func ConfirmDeletion(ctx context.Context, pods []types.Pod) (bool, error) {
	// Print pods to be deleted
	logger.InfofCtx(ctx, "Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.InfofCtx(ctx, "\t-> %s\n", pod.Name)
	}

	// Confirm deletion
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfolnCtx(ctx, "Deletion cancelled")

		return false, nil
	}

	return true, nil
}
