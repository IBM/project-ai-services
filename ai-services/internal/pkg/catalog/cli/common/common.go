package common

import (
	"context"
	"fmt"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// GetCatalogPods return the list of catalog pod.
func GetCatalogPods(ctx context.Context, rt runtime.Runtime) ([]types.Pod, error) {
	// Check if catalog pods exist
	pods, err := rt.ListPods(map[string][]string{
		"label": {fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, catalogConstants.CatalogAppName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfolnCtx(ctx, "Catalog service is not deployed")

		return nil, nil
	}

	logger.InfofCtx(ctx, "Found %d catalog pod(s)\n", len(pods))

	return pods, nil
}

func IsCatalogInstalled(ctx context.Context, helmClient *helm.Helm, catalog, namespace string) (bool, error) {
	exists, err := helmClient.IsReleaseExist(catalog)
	if err != nil {
		return false, fmt.Errorf("failed to check if catalog exists: %w", err)
	}

	if !exists {
		logger.InfofCtx(ctx, "Catalog '%s' does not exist in namespace '%s'\n", catalog, namespace)

		return false, nil
	}

	return true, nil
}
