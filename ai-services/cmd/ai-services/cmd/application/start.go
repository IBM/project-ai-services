package application

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
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

Arguments
  [name]: Application name (required)

Note: Logs are streamed only when a single pod is specified, and only after the pod has started.

Note: Supported for podman runtime only.
`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
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

		rt := vars.RuntimeFactory.GetRuntimeType()

		// For podman runtime with default mode, validate application name using catalog API
		if !legacyStart && rt == types.RuntimeTypePodman {
			appClient, err := catalogClient.NewApplicationClient()
			if err != nil {
				return fmt.Errorf("failed to create application client: %w", err)
			}
			if _, err := utils.GetAppByName(appClient, applicationName); err != nil {
				return err
			}
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

		return app.Start(opts)
	},
}

func init() {
	startCmd.Flags().BoolVar(&legacyStart, "legacy", false, "Use legacy application start implementation")
	startCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to start (optional)\nCan be specified multiple times: --pod pod1 --pod pod2\nOr comma-separated: --pod pod1,pod2")
	startCmd.Flags().BoolVar(&skipLogs, "skip-logs", false, "Skip displaying logs after starting the pod")
	startCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
}
