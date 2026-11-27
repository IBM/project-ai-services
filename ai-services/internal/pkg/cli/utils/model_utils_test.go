package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// fakeLister serves the given pages of models.
type fakeLister struct {
	pages [][]models.ModelListItem
}

func (f fakeLister) ListModels(_ context.Context, params *catalogClient.ListModelsParams) (*models.ModelListResponse, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}

	return &models.ModelListResponse{
		Data:       f.pages[page-1],
		Pagination: types.PaginationMetadata{Page: page, HasNext: page < len(f.pages)},
	}, nil
}

func item(id, name string) models.ModelListItem {
	return models.ModelListItem{ModelBase: models.ModelBase{ID: id, Name: name}}
}

func TestGetModelByName(t *testing.T) {
	tests := []struct {
		name    string
		pages   [][]models.ModelListItem
		lookup  string
		wantID  string
		wantErr bool
	}{
		{
			name:   "exact match among substring matches",
			pages:  [][]models.ModelListItem{{item("1", "granite-llm-large"), item("2", "granite-llm")}},
			lookup: "granite-llm",
			wantID: "2",
		},
		{
			name:   "case-insensitive",
			pages:  [][]models.ModelListItem{{item("1", "Granite-LLM")}},
			lookup: "granite-llm",
			wantID: "1",
		},
		{
			name:   "match on a later page",
			pages:  [][]models.ModelListItem{{item("1", "granite-a")}, {item("2", "granite")}},
			lookup: "granite",
			wantID: "2",
		},
		{
			name:    "no match",
			pages:   [][]models.ModelListItem{{item("1", "granite-llm-large")}},
			lookup:  "granite-llm",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetModelByName(context.Background(), fakeLister{pages: tt.pages}, tt.lookup)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found")

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}
