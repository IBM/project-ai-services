package application

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Lists the offered application templates and their supported parameters",
	Long:  `Retrieves information about the offered application templates and their supported parameters`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: vars.RuntimeFactory.GetRuntimeType()})

		appTemplateNames, err := tp.ListApplications(hiddenTemplates)
		if err != nil {
			return fmt.Errorf("failed to list application templates: %w", err)
		}

		if len(appTemplateNames) == 0 {
			logger.Infoln("No application templates found.")

			return nil
		}

		// sort appTemplateNames alphabetically
		sort.Strings(appTemplateNames)

		logger.Infoln("Available application templates:")
		for _, name := range appTemplateNames {
			appTemplatesParametersWithDescription, err := tp.ListApplicationTemplateValues(name)
			if err != nil {
				logger.Errorf("failed to list application template values: %v", err)

				continue
			}

			logger.Infof("- %s\n", name)
			metadata, err := tp.LoadMetadata(name, false)
			if err != nil {
				logger.Errorf("failed to load application metadata: %v", err)

				continue
			}
			if metadata.Description != "" {
				logger.Infof("  Description: %s", metadata.Description)
			}

			logger.Infoln("\n  Supported Parameters:")
			if len(appTemplatesParametersWithDescription) == 0 {
				logger.Infoln("\t" + "NONE")
			}

			for k, v := range appTemplatesParametersWithDescription {
				logger.Infoln("\t" + k + ":  " + v)
			}
			cmd.Println()
		}

		return nil
	},
}
