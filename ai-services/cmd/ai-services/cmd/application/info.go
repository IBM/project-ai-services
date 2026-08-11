package application

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	legacyInfo bool
)

var infoCmd = &cobra.Command{
	Use:   "info [name]",
	Short: "Application info",
	Long: `Displays the information about the running application

Arguments:
  [name] : Application name (required)
	`,
	Example: `  # Display application information from podman runtime
  ai-services application info rag --runtime podman
  
  # Display application information from openshift runtime
  ai-services application info rag --runtime openshift
  `,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.InfoOptions{
			Name:   applicationName,
			Legacy: legacyInfo,
		}

		return app.Info(opts)
	},
}

func init() {
	infoCmd.Flags().BoolVar(&legacyInfo, "legacy", false, "Use legacy application info implementation")
}
