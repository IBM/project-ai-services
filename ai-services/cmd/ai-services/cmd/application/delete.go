package application

import (
	"fmt"
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
	skipCleanup        bool
	deleteTimeout      time.Duration
	experimentalDelete bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete an application",
	Long: `Deletes an application and all associated resources.

Arguments
  [name]: Application name (required)`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildDeleteFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		appName := args[0]
		if !experimentalDelete {
			return utils.VerifyAppName(appName)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// When experimentalDelete is true and runtime is podman, validate application name using catalog API
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalDelete && rt == types.RuntimeTypePodman {
			return deleteApplication(applicationName)
		}

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

	},
}

func init() {
	initDeleteCommonFlags()
	initDeleteOpenShiftFlags()
}

func initDeleteCommonFlags() {
	deleteCmd.Flags().BoolVar(&skipCleanup, appFlags.Delete.SkipCleanup, false, "Skip deleting application data (default=false)")
	deleteCmd.Flags().BoolVarP(&autoYes, appFlags.Delete.AutoYes, "y", false, "Automatically accept all confirmation prompts (default=false)")
	deleteCmd.Flags().BoolVar(&experimentalDelete, "experimental", false, "Include experimental application delete")
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

	deleteParams := catalogClient.DeleteApplicationParams{
		SkipCleanup: skipCleanup,
		AutoYes:     autoYes,
	}

	if err := appClient.DeleteApplication(app.ID, &deleteParams); err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	logger.Infof("Application %s deleted successfully.", appName)

	return nil
}
