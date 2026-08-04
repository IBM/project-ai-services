package podman

import (
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	rt "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// DisplayCatalogInfo displays detailed information about the catalog service.
func DisplayCatalogInfo() error {
	// Initialize runtime
	runtime, err := rt.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Step 1: Check if catalog pod exists
	listFilters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", constants.CatalogAppName)},
	}

	pods, err := runtime.ListPods(listFilters)
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

	// Step 3: Fetch route information
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")
	routeDomains, httpsPort, err := GetCatalogRouteInfo(runtime)
	if err != nil {
		logger.Errorf("failed to get route info: %v\n", err)
		// Continue with basic info display
		if err := helpers.PrintInfo(tp, runtime, constants.CatalogAppName, catalogTemplate); err != nil {
			logger.Errorf("failed to display info: %v\n", err)
		}

		return nil
	}

	// Step 4: Read and print the info.md file with route information
	if err := helpers.PrintInfoWithProxy(tp, runtime, constants.CatalogAppName, catalogTemplate, routeDomains, httpsPort); err != nil {
		// not failing overall info command if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)

		return nil
	}

	return nil
}

// GetCatalogRouteInfo retrieves route domains and HTTPS port for the catalog service.
// This orchestrates: deployContext gets pod name and route info from templates,
// caddy.Context queries Caddy for route domains and HTTPS port.
func GetCatalogRouteInfo(rt *rt.PodmanClient) (map[string]string, string, error) {
	// Create deployment context to access templates
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create deployment context: %w", err)
	}

	// Get Caddy pod name from templates
	caddyPodName, err := deployCtx.GetCaddyPodName()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy pod name: %w", err)
	}

	// Extract route infos from deployment context
	routeInfos, err := deployCtx.ExtractRouteInfos()
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract route infos: %w", err)
	}

	// Create Caddy context (domain suffix not needed for querying existing routes)
	caddyCtx := caddy.NewContext(caddyPodName, "")

	// Use caddy package to get route info
	return caddy.GetCatalogRouteInfo(caddyCtx, rt, routeInfos)
}

// Made with Bob
