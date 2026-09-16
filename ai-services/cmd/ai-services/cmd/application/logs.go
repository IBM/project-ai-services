package application

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	podName           string
	containerNameOrID string
	legacyLogs        bool
)

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Application pod logs",
	Long: `Displays logs from an application pod

Arguments:
  [name] : Application name (required)`,
	Example: `  # Display logs
  ai-services application logs rag --pod mypod

  # Display logs with explicit runtime
  ai-services application logs rag --pod mypod --runtime podman

  # Display logs from a specific container in a pod
  ai-services application logs rag --pod mypod --container mycontainer

  # Display logs using legacy implementation (requires --runtime)
  ai-services application logs rag --pod mypod --legacy --runtime podman

  # Display logs from an OpenShift application
  ai-services application logs rag --pod mypod --runtime openshift`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --runtime is required for legacy logs; the catalog path derives it from the Worker record.
		if legacyLogs && runtimeType == "" {
			return fmt.Errorf("required flag(s) \"runtime\" not set (required with --legacy)")
		}

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

		ctx := cmd.Context()

		var rt runtimeTypes.RuntimeType
		var namespace string

		if !legacyLogs {
			// Default: resolve runtime and namespace from the catalog Worker record.
			var err error
			rt, err = resolveRuntimeForApp(ctx, applicationName, runtimeType)
			if err != nil {
				return err
			}

			appClient, err := catalogClient.NewApplicationClient(ctx)
			if err != nil {
				return fmt.Errorf("failed to create application client: %w", err)
			}
			app, err := utils.GetAppByName(ctx, appClient, applicationName)
			if err != nil {
				return err
			}
			appID, err := uuid.Parse(app.ID)
			if err != nil {
				return fmt.Errorf("invalid application ID %q: %w", app.ID, err)
			}
			namespace = catalogutils.AppNamespace(appID)
		} else {
			// Legacy path: requires --runtime (enforced in PreRunE).
			rt = vars.RuntimeFactory.GetRuntimeType()
			namespace = applicationName
		}

		factory := application.NewFactory(rt)
		appInst, err := factory.Create(namespace)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		return appInst.Logs(ctx, appTypes.LogsOptions{
			PodName:           podName,
			ContainerNameOrID: containerNameOrID,
		})
	},
}

func init() {
	initLogsCommonFlags()
}

func initLogsCommonFlags() {
	logsCmd.Flags().BoolVar(&legacyLogs, appFlags.Logs.Legacy, false, "Use legacy application logs implementation")
	logsCmd.Flags().StringVar(&podName, appFlags.Logs.Pod, "", "Pod name to show logs from (required)")
	logsCmd.Flags().StringVar(&containerNameOrID, appFlags.Logs.Container, "", "Container logs to show logs from (Optional)")
	_ = logsCmd.MarkFlagRequired(appFlags.Logs.Pod)
}

// buildLogsFlagValidator creates and configures the flag validator for the logs command.
func buildLogsFlagValidator() *flagvalidator.FlagValidator {
	// Use the package-level runtimeType flag value (may be empty when --runtime is
	// omitted; runtime will be resolved from the application Worker in RunE).
	// All logs flags are common-scoped so the validator does not need a specific
	// runtime type to accept them.
	builder := flagvalidator.NewFlagValidatorBuilder(runtimeTypes.RuntimeType(runtimeType))

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Logs.Pod, nil).
		AddCommonFlag(appFlags.Logs.Container, nil).
		AddCommonFlag(appFlags.Logs.Legacy, nil)

	return builder.Build()
}
