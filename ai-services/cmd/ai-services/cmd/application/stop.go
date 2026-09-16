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
	stopPodNames []string
	legacyStop   bool
)

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stops the running application",
	Long: `Stops a running application by name.

Arguments:
  [name] : Application name (required)

Note:
  - Supported for podman runtime only.
`,
	Example: `  # Stop an application (runtime resolved from worker)
  ai-services application stop rag

  # Stop an application with explicit runtime
  ai-services application stop rag --runtime podman

  # Stop specific pods in an application
  ai-services application stop rag --pod pod1 --pod pod2

  # Stop specific pods using comma-separated list
  ai-services application stop rag --pod pod1,pod2

  # Stop with auto-accept confirmation prompts
  ai-services application stop rag --yes

  # Stop using legacy implementation (requires --runtime)
  ai-services application stop rag --legacy --runtime podman`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --runtime is required for legacy stop; the catalog path derives it from the Worker record.
		if legacyStop && runtimeType == "" {
			return fmt.Errorf("required flag(s) \"runtime\" not set (required with --legacy)")
		}

		// stop is only supported for podman runtime.
		if runtimeType != "" && runtimeType != string(types.RuntimeTypePodman) {
			return fmt.Errorf("stop is only supported for podman runtime")
		}

		var err error
		stopPodNames, err = cmd.Flags().GetStringSlice("pod")
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

		if !legacyStop {
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

		opts := appTypes.StopOptions{
			Name:     applicationName,
			PodNames: stopPodNames,
			AutoYes:  autoYes,
			Legacy:   legacyStop,
		}

		return app.Stop(ctx, opts)
	},
}

func init() {
	stopCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to stop (optional)\nCan be specified multiple times: --pod pod1 --pod pod2\nOr comma-separated: --pod pod1,pod2")
	stopCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
	stopCmd.Flags().BoolVar(&legacyStop, "legacy", false, "Use legacy application stop implementation")
}
