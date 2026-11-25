package deployment

import (
	"context"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

const validWatsonxKey = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQR" // 44 chars

// The /tokenize passthrough is registered by default for every remote model with an api_base.
func TestPlanRemote_TokenizePassthroughByDefault(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/granite",
		Params:         map[string]any{"api_base": "http://vllm.example.com:8000"},
	})
	require.NoError(t, err)
	assert.Equal(t, "http://vllm.example.com:8000/tokenize", plan.Route.TokenizeURL)
}

// watsonx has a fixed tokenization endpoint, so its target is not derived from api_base.
func TestPlanRemote_WatsonxTokenizeTarget(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeRemote,
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

func TestLoadLocalValues_ParamsCannotOverrideProfileHardware(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	podmanProvider, err := provider.WithRuntime("podman")
	require.NoError(t, err)

	values, err := loadLocalValues(podmanProvider, apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeLocal,
		ComponentType:  "llm",
		ProviderID:     "vllm-spyre",
		Name:           "test-model",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		Profile:        "standard",
		Params:         map[string]any{"maxModelLen": 1},
	})
	require.NoError(t, err)
	assert.Equal(t, 32768, values["maxModelLen"], "profile hardware values must not be overridden by params")
}

// A vLLM api_base may be given with or without /v1: the plan keeps the bare URL (used for the
// /v1/models lookup and the stored metadata) and the LiteLLM route always ends in /v1.
func TestPlanRemote_VLLMAPIBaseNormalized(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	for _, in := range []string{"http://h:8000", "http://h:8000/", "http://h:8000/v1", "http://h:8000/v1/"} {
		plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
			DeploymentType: constants.DeploymentTypeRemote,
			ComponentType:  "llm",
			Name:           "remote-vllm",
			ProviderID:     "hosted_vllm",
			Model:          "ibm/granite",
			Params:         map[string]any{"api_base": in},
		})
		require.NoError(t, err, in)
		assert.Equal(t, "http://h:8000", plan.Req.Params["api_base"], in)
		assert.Equal(t, "http://h:8000/v1", plan.Route.Params["api_base"], in)
		assert.Equal(t, "http://h:8000/tokenize", plan.Route.TokenizeURL, in)
	}
}

// Planning has no side effects: the caller's params map is left as given, while the plan carries
// the normalized copy.
func TestPlanRemote_DoesNotMutateRequestParams(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	params := map[string]any{"api_base": "http://h:8000/v1/", "maxBatchSize": 64}
	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/granite",
		Params:         params,
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"api_base": "http://h:8000/v1/", "maxBatchSize": 64}, params)
	assert.Equal(t, "http://h:8000", plan.Req.Params["api_base"])
}

// maxBatchSize is a platform-side setting: it must not reach the gateway route's litellm_params,
// while the request params (used for the stored metadata) keep it.
func TestPlanRemote_MaxBatchSizeNotForwardedToRoute(t *testing.T) {
	planner := NewModelPlanner(setupTestCatalogProvider(t), worker.NewResolver(nil))

	plan, err := planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/granite",
		Params:         map[string]any{"api_base": "http://h:8000", "maxBatchSize": 64},
	})
	require.NoError(t, err)
	assert.NotContains(t, plan.Route.Params, constants.ParamMaxBatchSize)
	assert.Equal(t, 64, plan.Req.Params[constants.ParamMaxBatchSize], "request params keep it for metadata")
}
