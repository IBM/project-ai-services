package application

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Lists the offered application templates",
	Long:  `Retrieves information about the offered application templates`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appTemplateNames, err := helpers.FetchApplicationTemplatesNames()
		if err != nil {
			return fmt.Errorf("failed to list application templates: %w", err)
		}

		if len(appTemplateNames) == 0 {
			cmd.PrintErrln("No application templates found.")
			return nil
		}

		// sort appTemplateNames alphabetically
		sort.Strings(appTemplateNames)

		appTemplateNameswithCustomArgs := helpers.FetchCustomArgsFromMetadata(appTemplateNames)

		cmd.Printf("Available Application Templates (%d total)\n", len(appTemplateNames))
		cmd.Println(strings.Repeat("=", 42))
		cmd.Println()

		for _, app := range appTemplateNames {
			args := appTemplateNameswithCustomArgs[app]

			cmd.Printf("Application: %s\n", app)

			if len(args) == 0 {
				cmd.Println("Supported Params: (none)")
			} else {
				cmd.Printf("Supported Params: %s\n", strings.Join(args, ", "))
				cmd.Println()
			}

			cmd.Println(strings.Repeat("-", 42))
			cmd.Println()
		}
		return nil
	},
}
