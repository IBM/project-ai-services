package validators

import (
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
)

func ValidateAppTemplateExist(tp templates.Template, templateName string) error {
	// Fetch all the application Template names
	appTemplateNames, err := tp.ListApplications()
	if err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}

	if !slices.Contains(appTemplateNames, templateName) {
		return fmt.Errorf("application template '%s' does not exist", templateName)
	}

	return nil
}
