package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// UninstallCatalog removes the catalog service and all associated resources.
func UninstallCatalog(ctx context.Context, autoYes bool) error {
	s := spinner.New("Removing catalog service...")
	s.Start(ctx)

	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		s.Fail("failed to initialize podman client")

		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Check if catalog pods exist
	existingPods, err := helpers.CheckExistingPodsForApplication(rt, catalogAppName)
	if err != nil {
		s.Fail("failed to check existing pods")

		return fmt.Errorf("failed to check existing pods: %w", err)
	}

	if len(existingPods) == 0 {
		s.Stop("No catalog service found")
		logger.Infoln("Catalog service is not deployed")

		return nil
	}

	s.Stop(fmt.Sprintf("Found %d catalog pod(s)", len(existingPods)))

	// Confirm deletion if not auto-yes
	if !autoYes {
		logger.Infoln("\nThe following pods will be removed:")
		for _, pod := range existingPods {
			logger.Infof("  - %s\n", pod)
		}

		confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
		if err != nil {
			return fmt.Errorf("failed to get confirmation: %w", err)
		}
		if !confirmed {
			return fmt.Errorf("operation cancelled by user")
		}
	}

	// Start removal spinner
	s = spinner.New("Removing catalog pods...")
	s.Start(ctx)

	// Remove all catalog pods
	force := true
	for _, podName := range existingPods {
		logger.Infof("Removing pod: %s\n", podName)
		if err := rt.DeletePod(podName, &force); err != nil {
			s.Fail(fmt.Sprintf("failed to remove pod: %s", podName))

			return fmt.Errorf("failed to remove pod %s: %w", podName, err)
		}
	}

	s.Stop("Catalog service removed successfully")

	return nil
}

// Made with Bob
