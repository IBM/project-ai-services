package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// modelListPageSize is the maximum page size accepted by GET /api/v1/models.
const modelListPageSize = 100

// modelLister lists models; satisfied by *catalogClient.ModelClient.
type modelLister interface {
	ListModels(ctx context.Context, params *catalogClient.ListModelsParams) (*models.ModelListResponse, error)
}

// GetModelByName resolves a model name to its list entry. The server search is a substring
// match, so every page of results is scanned for a case-insensitive exact match on the name.
func GetModelByName(ctx context.Context, modelClient modelLister, name string) (*models.ModelListItem, error) {
	for page := 1; ; page++ {
		resp, err := modelClient.ListModels(ctx, &catalogClient.ListModelsParams{
			Page:     page,
			PageSize: modelListPageSize,
			Search:   name,
		})
		if err != nil {
			return nil, err
		}

		for i := range resp.Data {
			if strings.EqualFold(resp.Data[i].Name, name) {
				return &resp.Data[i], nil
			}
		}

		if !resp.Pagination.HasNext {
			break
		}
	}

	return nil, fmt.Errorf("model with name '%s' not found", name)
}
