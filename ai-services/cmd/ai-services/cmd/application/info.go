package application

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

var infoCmd = &cobra.Command{
	Use:   "info [name]",
	Short: "Application info",
	Long: `Displays the information about the running application
		Arguments
		- [name]: Application name (Required)
	`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		runtimeType, err := cmd.Flags().GetString("runtime")
		if err != nil {
			return fmt.Errorf("failed to get runtime flag: %w", err)
		}
		rt := types.RuntimeType(runtimeType)

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create()
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.InfoOptions{
			Name: applicationName,
		}

		return app.Info(opts)
	},
}
