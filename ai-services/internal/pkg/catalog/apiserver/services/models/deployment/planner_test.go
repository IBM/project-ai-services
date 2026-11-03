package deployment

import (
	"context"
	"testing"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The /tokenize passthrough is registered by default for every remote model with an api_base.
func TestPlanRemote_TokenizePassthroughByDefault(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t))

	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/granite",
		Params:         map[string]any{"api_base": "http://vllm.example.com:8000/v1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "http://vllm.example.com:8000/tokenize", plan.Route.TokenizeURL)
}
