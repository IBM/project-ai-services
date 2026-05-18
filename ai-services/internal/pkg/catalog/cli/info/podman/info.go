package podman

import (
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// DisplayCatalogInfo displays detailed information about the catalog service.
func DisplayCatalogInfo() error {
	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Step 1: Check if catalog pod exists
	listFilters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", constants.CatalogAppName)},
	}

	pods, err := rt.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// If there exists no pod for catalog, then inform user
	if len(pods) == 0 {
		logger.Infof("Catalog service is not configured or running.\n")
		logger.Infof("Run 'ai-services catalog configure --runtime podman' to set up the catalog service.\n")

		return nil
	}

	logger.Infoln("Catalog Service Name: " + constants.CatalogAppName)

	// Step 2: Fetch and print the template and version label values
	catalogTemplate := pods[0].Labels[string(vars.TemplateLabel)]
	logger.Infoln("Catalog Template: " + catalogTemplate)

	version := pods[0].Labels[string(vars.VersionLabel)]
	logger.Infoln("Version: " + version)

	// Step 3: Read and print the info.md file
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	if err := helpers.PrintInfo(tp, rt, constants.CatalogAppName, catalogTemplate); err != nil {
		// not failing overall info command if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)

		return nil
	}

	return nil
}

// Made with Bob
