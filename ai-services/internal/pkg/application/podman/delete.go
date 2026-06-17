package podman

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Delete removes an application and its associated resources.
func (p *PodmanApplication) Delete(_ context.Context, opts appTypes.DeleteOptions) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	app, err := cliUtils.GetAppByName(appClient, opts.Name)
	if err != nil {
		return err
	}
	if app == nil {
		return fmt.Errorf("application not found: %s", opts.Name)
	}

	if !opts.AutoYes {
		confirmDelete, err := p.deleteConfirmation()
		if err != nil {
			return err
		}
		if !confirmDelete {
			logger.Infoln("Deletion cancelled")

			return nil
		}
	}

	deleteParams := catalogClient.DeleteApplicationParams{
		KeepData: opts.SkipCleanup,
	}

	if err := appClient.DeleteApplication(app.ID, &deleteParams); err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	// Poll to verify deletion is complete
	logger.Infof("Waiting for application %s to be deleted...\n", opts.Name)
	if err := p.waitForApplicationDeletion(appClient, app.ID); err != nil {
		return fmt.Errorf("failed to verify application deletion: %w", err)
	}

	logger.Infof("Application %s deleted successfully.", opts.Name)

	return nil
}

// deleteConfirmation prompts the user to confirm deletion.
func (p *PodmanApplication) deleteConfirmation() (bool, error) {
	confirmActionPrompt := "Are you sure you want to delete the application? "
	confirmDelete, err := utils.ConfirmAction(confirmActionPrompt)
	if err != nil {
		return confirmDelete, fmt.Errorf("failed to take user input: %w", err)
	}

	return confirmDelete, nil
}

// waitForApplicationDeletion polls the application status until it's fully deleted.
func (p *PodmanApplication) waitForApplicationDeletion(appClient *catalogClient.ApplicationClient, appID string) error {
	const (
		pollInterval = 5 * time.Second
		maxAttempts  = 12
	)

	for range maxAttempts {
		// Check if application still exists via API
		app, err := appClient.GetApplication(appID)
		if err != nil {
			// Check if it's an HTTPError with 404 status code
			var httpErr *catalogClient.HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				// Application not found (HTTP 404) - successfully deleted
				return nil
			}

			return fmt.Errorf("failed to fetch application: %w", err)
		}

		// If application exists, check its status
		if app != nil {
			logger.Infof("Application status: %s, message: %s\n", app.Status, app.Message)
			// Application still exists, continue polling
		}

		// Wait before next poll
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for application deletion after %v", maxAttempts*pollInterval)
}
