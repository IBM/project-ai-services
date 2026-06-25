package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	skipCleanup   bool
	deleteTimeout time.Duration
	legacyDelete  bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete an application",
	Long: `Deletes an application and all associated resources.

Arguments:
  [name] : Application name (required)`,
	Example: `  # Delete an application from podman runtime
  ai-services application delete rag --runtime podman
  
  # Delete an application from openshift runtime
  ai-services application delete rag --runtime openshift
  `,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildDeleteFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		appName := args[0]
		if legacyDelete {
			return utils.VerifyAppName(appName)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// When legacyDelete is true and runtime is podman, use the older/stable code path
		// For openshift runtime, always use the older/stable code path regardless of legacy flag
		if legacyDelete && rt == types.RuntimeTypePodman {
			// Create application instance using factory
			factory := application.NewFactory(rt)
			app, err := factory.Create(applicationName)
			if err != nil {
				return fmt.Errorf("failed to create application instance: %w", err)
			}

			opts := appTypes.DeleteOptions{
				Name:        applicationName,
				AutoYes:     autoYes,
				SkipCleanup: skipCleanup,
				Timeout:     deleteTimeout,
			}

			return app.Delete(cmd.Context(), opts)
		}

		// Default: use new implementation (validate application name using catalog API)
		// For openshift runtime, always use the older/stable code path
		if rt == types.RuntimeTypePodman {
			return deleteApplication(applicationName)
		}

		// OpenShift runtime uses the older implementation
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.DeleteOptions{
			Name:        applicationName,
			AutoYes:     autoYes,
			SkipCleanup: skipCleanup,
			Timeout:     deleteTimeout,
		}

		return app.Delete(cmd.Context(), opts)

	},
}

func init() {
	initDeleteCommonFlags()
	initDeleteOpenShiftFlags()
}

func initDeleteCommonFlags() {
	deleteCmd.Flags().BoolVar(&skipCleanup, appFlags.Delete.SkipCleanup, false, "Skip deleting application data (default=false)")
	deleteCmd.Flags().BoolVarP(&autoYes, appFlags.Delete.AutoYes, "y", false, "Automatically accept all confirmation prompts (default=false)")
	deleteCmd.Flags().BoolVar(&legacyDelete, "legacy", false, "Use legacy application delete implementation")
}

func initDeleteOpenShiftFlags() {
	deleteCmd.Flags().DurationVar(
		&deleteTimeout,
		appFlags.Delete.Timeout,
		0, // default
		"Timeout for the operation (e.g. 10s, 2m, 1h).\n"+
			"Note: Supported for openshift runtime only.\n",
	)
}

// buildDeleteFlagValidator creates and configures the flag validator for the delete command.
func buildDeleteFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Delete.SkipCleanup, nil).
		AddCommonFlag(appFlags.Delete.AutoYes, nil)

	// Register OpenShift-specific flags
	builder.
		AddOpenShiftFlag(appFlags.Delete.Timeout, nil)

	return builder.Build()
}

func deleteApplication(appName string) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}
	app, err := cliUtils.GetAppByName(appClient, appName)
	if err != nil {
		return err
	}
	if app == nil {
		return fmt.Errorf("application not found: %s", appName)
	}

	if !autoYes {
		confirmDelete, err := deleteConfirmation()
		if err != nil {
			return err
		}
		if !confirmDelete {
			logger.Infoln("Deletion cancelled")

			return nil
		}
	}

	deleteParams := catalogClient.DeleteApplicationParams{
		KeepData: skipCleanup,
	}

	// Retry deletion if it fails
	logger.Infof("Deleting application %s...\n", appName)
	err = utils.Retry(context.Background(), vars.RetryCount, vars.RetryInterval, nil, func() error {
		return appClient.DeleteApplication(app.ID, &deleteParams)
	})
	if err != nil {
		return fmt.Errorf("failed to delete application after %d retries: %w", vars.RetryCount, err)
	}

	// Poll to verify deletion is complete
	logger.Infof("Waiting for application %s to be deleted...\n", appName)
	if err := waitForApplicationDeletion(appClient, app.ID); err != nil {
		return fmt.Errorf("failed to verify application deletion: %w", err)
	}

	logger.Infof("Application %s deleted successfully.", appName)

	return nil
}

// waitForApplicationDeletion polls the application status until it's fully deleted.
func waitForApplicationDeletion(appClient *catalogClient.ApplicationClient, appID string) error {
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

func deleteConfirmation() (bool, error) {
	confirmActionPrompt := "Are you sure you want to delete the application? "
	confirmDelete, err := utils.ConfirmAction(confirmActionPrompt)
	if err != nil {
		return confirmDelete, fmt.Errorf("failed to take user input: %w", err)
	}

	return confirmDelete, nil
}
