package application

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	skipLogs      bool
	startPodNames []string
	autoYes       bool
	legacyStart   bool
)

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start an application",
	Long: `Starts an application by name.

Arguments:
  [name] : Application name (required)

Note:
  - Logs are streamed only when a single pod is specified, and only after the pod has started.
  - Supported for podman runtime only.
`,
	Example: `  # Start an application (runtime resolved from worker)
  ai-services application start rag

  # Start an application with explicit runtime
  ai-services application start rag --runtime podman

  # Start an application and skip logs
  ai-services application start rag --skip-logs

  # Start specific pods in an application
  ai-services application start rag --pod pod1 --pod pod2

  # Start specific pods using comma-separated list
  ai-services application start rag --pod pod1,pod2

  # Start a single pod and view logs
  ai-services application start rag --pod mypod

  # Start with auto-accept confirmation prompts
  ai-services application start rag --yes

  # Start using legacy implementation (requires --runtime)
  ai-services application start rag --legacy --runtime podman`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --runtime is required for legacy start; the catalog path derives it from the Worker record.
		if legacyStart && runtimeType == "" {
			return fmt.Errorf("required flag(s) \"runtime\" not set (required with --legacy)")
		}

		// start is only supported for podman runtime.
		if runtimeType != "" && runtimeType != string(types.RuntimeTypePodman) {
			return fmt.Errorf("start is only supported for podman runtime")
		}

		var err error
		startPodNames, err = cmd.Flags().GetStringSlice("pod")
		if err != nil {
			return fmt.Errorf("failed to parse --pod flag: %w", err)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		ctx := cmd.Context()

		var rt types.RuntimeType

		if !legacyStart {
			// Default: resolve runtime from the application's Worker record in the catalog.
			var err error
			rt, err = resolveRuntimeForApp(ctx, applicationName, runtimeType)
			if err != nil {
				return err
			}

			// Validate application name using catalog API.
			appClient, err := catalogClient.NewApplicationClient(ctx)
			if err != nil {
				return fmt.Errorf("failed to create application client: %w", err)
			}
			if _, err := utils.GetAppByName(ctx, appClient, applicationName); err != nil {
				return err
			}
		} else {
			rt = vars.RuntimeFactory.GetRuntimeType()
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		// start application with options
		opts := appTypes.StartOptions{
			Name:     applicationName,
			PodNames: startPodNames,
			AutoYes:  autoYes,
			SkipLogs: skipLogs,
			Legacy:   legacyStart,
		}

		return app.Start(ctx, opts)
	},
}

func init() {
	startCmd.Flags().BoolVar(&legacyStart, "legacy", false, "Use legacy application start implementation")
	startCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to start (optional)\nCan be specified multiple times: --pod pod1 --pod pod2\nOr comma-separated: --pod pod1,pod2")
	startCmd.Flags().BoolVar(&skipLogs, "skip-logs", false, "Skip displaying logs after starting the pod")
	startCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
}
