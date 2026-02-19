package application

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var output string

func init() {
	psCmd.Flags().StringVarP(
		&output,
		"output",
		"o",
		"",
		"Output format (e.g., wide)",
	)
}

func isOutputWide() bool {
	return strings.ToLower(output) == "wide"
}

var psCmd = &cobra.Command{
	Use:   "ps [name]",
	Short: "Lists all or specified running application(s)",
	Long: `Retrieves information about all the running applications if no name is provided
Lists information about a specific application if the name is provided
Arguments
  [name]: Application name (optional)
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		var applicationName string
		if len(args) > 0 {
			applicationName = args[0]
		}

		rt := vars.RuntimeFactory.GetRuntimeType()

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.ListOptions{
			ApplicationName: applicationName,
			OutputWide:      isOutputWide(),
		}

		_, err = app.List(opts)
		if err != nil {
			return fmt.Errorf("failed to fetch application: %w", err)
		}

		return nil
	},
}
