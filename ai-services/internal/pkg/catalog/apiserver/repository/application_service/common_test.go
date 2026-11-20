package applicationservice

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	deployment "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureComponentRepo records the rows passed to Insert; every other method would panic.
type captureComponentRepo struct {
	dbrepo.ComponentRepository
	inserted []*models.Component
}

func (r *captureComponentRepo) Insert(_ context.Context, c *models.Component) error {
	r.inserted = append(r.inserted, c)

	return nil
}

func insertOne(t *testing.T, comp *deployment.ComponentPlan) *models.Component {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	repo := &captureComponentRepo{}
	s := &ApplicationServiceBase{ComponentRepo: repo, Provider: provider}
	plan := &deployment.DeploymentPlan{
		RuntimeType: "podman",
		Components:  map[string]*deployment.ComponentPlan{"h": comp},
	}

	_, err = s.insertComponentRecords(context.Background(), plan, "tester", nil)
	require.NoError(t, err)
	require.Len(t, repo.inserted, 1)

	return repo.inserted[0]
}

func TestInsertComponentRecords_Profile(t *testing.T) {
	const granite33 = "ibm-granite/granite-3.3-8b-instruct"

	t.Run("model component stores the resolved profile next to the model", func(t *testing.T) {
		row := insertOne(t, &deployment.ComponentPlan{
			ComponentType: "llm",
			ProviderID:    "vllm-spyre",
			Version:       "1.0.0",
			Params:        map[string]any{"model": granite33},
			Values:        map[string]any{"model": granite33, "profile": "standard"},
		})

		assert.Equal(t, granite33, row.Metadata["model"])
		assert.Equal(t, "standard", row.Metadata["profile"])
	})

	t.Run("profile is stored even when the request had no params", func(t *testing.T) {
		row := insertOne(t, &deployment.ComponentPlan{
			ComponentType: "llm",
			ProviderID:    "vllm-spyre",
			Version:       "1.0.0",
			Values:        map[string]any{"profile": "standard"},
		})

		assert.Equal(t, "standard", row.Metadata["profile"])
	})

	t.Run("sensitive params are still excluded", func(t *testing.T) {
		row := insertOne(t, &deployment.ComponentPlan{
			ComponentType: "llm",
			ProviderID:    "vllm-spyre",
			Version:       "1.0.0",
			Params:        map[string]any{"model": granite33, "apiKey": "secret"},
			Values:        map[string]any{"profile": "standard"},
		})

		assert.NotContains(t, row.Metadata, "apiKey")
		assert.Equal(t, "standard", row.Metadata["profile"])
	})

	t.Run("component without a profile gets none", func(t *testing.T) {
		row := insertOne(t, &deployment.ComponentPlan{
			ComponentType: "vector_store",
			ProviderID:    "opensearch",
			Version:       "1.0.0",
			Params:        map[string]any{},
			Values:        map[string]any{},
		})

		assert.NotContains(t, row.Metadata, "profile")
	})
}
