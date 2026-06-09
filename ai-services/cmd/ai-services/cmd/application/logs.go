package application

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var (
	podName           string
	containerNameOrID string
	experimentalLogs  bool
)

var logsCmd = &cobra.Command{
	Use: "logs [name]",
	Long: `Displays logs from an application pod
Arguments
[name]: Application name (required)`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildLogsFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		if podName == "" {
			return fmt.Errorf("pod name must be specified using --pod flag")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// When experimentalLogs is true and runtime is podman, validate application name using catalog API
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalLogs && rt == types.RuntimeTypePodman {
			if err := utils.ValidateApplicationName(applicationName); err != nil {
				return err
			}
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.LogsOptions{
			PodName:           podName,
			ContainerNameOrID: containerNameOrID,
		}

		return app.Logs(opts)
	},
}

func init() {
	initLogsCommonFlags()
}

func initLogsCommonFlags() {
	logsCmd.Flags().BoolVar(&experimentalLogs, "experimental", false, "Include experimental application templates")
	logsCmd.Flags().StringVar(&podName, appFlags.Logs.Pod, "", "Pod name to show logs from (required)")
	logsCmd.Flags().StringVar(&containerNameOrID, appFlags.Logs.Container, "", "Container logs to show logs from (Optional)")
	_ = logsCmd.MarkFlagRequired(appFlags.Logs.Pod)
}

// buildLogsFlagValidator creates and configures the flag validator for the logs command.
func buildLogsFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Logs.Pod, nil).
		AddCommonFlag(appFlags.Logs.Container, nil)

	return builder.Build()
}
