package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	aiconst "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	oc "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// DisplayCatalogInfo displays detailed information about the catalog service on OpenShift.
func DisplayCatalogInfo(ctx context.Context) error {
	// Initialize OpenShift client scoped to the catalog namespace
	runtime, err := oc.NewOpenshiftClientWithNamespace(constants.CatalogAppName)
	if err != nil {
		return fmt.Errorf("failed to initialize openshift client: %w", err)
	}

	// Step 1: Check if catalog pods exist in the namespace
	listFilters := map[string][]string{
		"label": {fmt.Sprintf("%s=%s", aiconst.ApplicationAnnotationKey, constants.CatalogAppName)},
	}

	pods, err := runtime.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfofCtx(ctx, "Catalog service is not configured or running.\n")
		logger.InfofCtx(ctx, "Run 'ai-services catalog configure --runtime openshift' to set up the catalog service.\n")

		return nil
	}

	logger.InfolnCtx(ctx, "Catalog Service Name: "+constants.CatalogAppName)

	// Step 2: Fetch and print the template and version label values
	catalogTemplate := pods[0].Labels[string(vars.TemplateLabel)]
	logger.InfolnCtx(ctx, "Catalog Template: "+catalogTemplate)

	version := pods[0].Labels[string(vars.VersionLabel)]
	logger.InfolnCtx(ctx, "Version: "+version)

	// Step 3: Read and print the info.md file
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	if err := helpers.PrintInfo(tp, runtime, constants.CatalogAppName, catalogTemplate); err != nil {
		// not failing overall info command if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)
	}

	return nil
}
