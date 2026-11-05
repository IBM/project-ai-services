package deployment

import (
	"context"
	"testing"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The /tokenize passthrough is registered by default for every remote model with an api_base.
func TestPlanRemote_TokenizePassthroughByDefault(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

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

// watsonx has a fixed tokenization endpoint, so its target is not derived from api_base.
func TestPlanRemote_WatsonxTokenizeTarget(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "remote-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-4-h-small",
		Params: map[string]any{
			"api_key":    validWatsonxKey,
			"project_id": "123e4567-e89b-12d3-a456-426614174000",
			"api_base":   "https://us-south.ml.cloud.ibm.com",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:4000/watsonx/ml/v1/text/tokenization", plan.Route.TokenizeURL)
}

func TestRemoteTokenizeURL(t *testing.T) {
	assert.Equal(t, watsonxTokenizeURL, remoteTokenizeURL("watsonx", nil), "watsonx ignores api_base")
	assert.Equal(t, "http://h:8000/tokenize", remoteTokenizeURL("hosted_vllm", map[string]any{"api_base": "http://h:8000/v1"}))
	assert.Empty(t, remoteTokenizeURL("hosted_vllm", map[string]any{}), "no api_base means no passthrough")
}
