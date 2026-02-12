package application

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Lists the offered application templates and their supported parameters",
	Long:  `Retrieves information about the offered application templates and their supported parameters`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

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
				return fmt.Errorf("failed to list application template values: %w", err)
			}
			logger.Infof("- %s\n", name)
			metadata, err := tp.LoadMetadata(name, false)
			if err != nil {
				return fmt.Errorf("failed to load application metadata: %w", err)
			}
			if metadata.Description != "" {
				logger.Infof("  Description: %s", metadata.Description)
			}
			logger.Infoln("\n  Supported Parameters:")
			for k, v := range appTemplatesParametersWithDescription {
				logger.Infoln("\t" + k + ":  " + v)
			}
		}

		return nil
	},
}
