package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	deploymentPollInterval = 5 * time.Second
	deploymentPollTimeout  = 5 * time.Minute
)

// ConfirmCatalogReset displays a warning about catalog service unavailability and prompts for user confirmation.
// The flagName parameter is used to customize the warning and confirmation messages.
// Returns true if user confirms, false if cancelled, or an error if confirmation fails.
func ConfirmCatalogReset(flagName string) (bool, error) {
	logger.WarningfCtx(context.Background(), "Resetting %s will reload the catalog pod, catalog service will be temporarily unavailable during this time!", flagName)

	// Confirm action
	confirmed, err := utils.ConfirmAction(fmt.Sprintf("\nDo you want to continue, with %s reset?", flagName))
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfofCtx(context.Background(), "Catalog %s reset cancelled", flagName)

		return false, nil
	}

	return true, nil
}

// WaitForDeploymentReady polls GetDeploymentStatus until all desired replicas are ready
// or the timeout is exceeded.
func WaitForDeploymentReady(ctx context.Context, rt runtime.Runtime, name string) error {
	logger.InfofCtx(ctx, "Waiting for deployment '%s' to become ready...", name)

	deadline := time.Now().Add(deploymentPollTimeout)
	ticker := time.NewTicker(deploymentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := rt.GetDeploymentStatus(name)
			if err != nil {
				return fmt.Errorf("failed to get deployment status: %w", err)
			}

			logger.DebugfCtx(ctx, "Deployment '%s': %d/%d replicas ready",
				name, status.ReadyReplicas, status.DesiredReplicas)

			if status.DesiredReplicas > 0 && status.ReadyReplicas == status.DesiredReplicas {
				logger.InfofCtx(ctx, "\nDeployment '%s' is ready", name)

				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("timed out after %s waiting for deployment '%s' to be ready "+
					"(%d/%d replicas ready)",
					deploymentPollTimeout, name, status.ReadyReplicas, status.DesiredReplicas)
			}
		}
	}
}
