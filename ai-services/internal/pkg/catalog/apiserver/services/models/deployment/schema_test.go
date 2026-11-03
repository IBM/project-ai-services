package deployment

import (
	"context"
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validWatsonxKey = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQR" // 44 chars

func podmanProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	p, err := setupTestCatalogProvider(t).WithRuntime("podman")
	require.NoError(t, err)

	return p
}

func TestValidateLocalParams(t *testing.T) {
	provider := podmanProvider(t)
	base := apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeLocal,
		ComponentType:  "llm",
		Name:           "granite-llm",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}

	t.Run("valid with no params", func(t *testing.T) {
		_, err := validateLocalParams(context.Background(), provider, base)
		require.NoError(t, err)
	})

	t.Run("valid with schema param", func(t *testing.T) {
		req := base
		req.Params = map[string]any{"apiKey": "secret"}
		_, err := validateLocalParams(context.Background(), provider, req)
		require.NoError(t, err)
	})

	t.Run("unknown provider is 404", func(t *testing.T) {
		req := base
		req.ProviderID = "nope"
		_, err := validateLocalParams(context.Background(), provider, req)
		requireStatus(t, err, http.StatusNotFound, "not found")
	})

	t.Run("unknown param is 400", func(t *testing.T) {
		req := base
		req.Params = map[string]any{"bogus": "x"}
		_, err := validateLocalParams(context.Background(), provider, req)
		requireStatus(t, err, http.StatusBadRequest, "Parameter validation failed")
	})

	t.Run("wrongly typed param is 400", func(t *testing.T) {
		req := base
		req.Params = map[string]any{"apiKey": 123}
		_, err := validateLocalParams(context.Background(), provider, req)
		requireStatus(t, err, http.StatusBadRequest, "Parameter validation failed")
	})
}

func TestValidateRemoteParams(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	ctx := context.Background()

	watsonx := apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeRemote,
		ComponentType:  "llm",
		Name:           "prod-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-4-h-small",
		Params: map[string]any{
			"api_key":    validWatsonxKey,
			"project_id": "123e4567-e89b-12d3-a456-426614174000",
			"api_base":   "https://us-south.ml.cloud.ibm.com",
		},
	}

	t.Run("watsonx valid; api_key is the sensitive field", func(t *testing.T) {
		sensitive, err := validateRemoteParams(ctx, provider, watsonx)
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"api_key": true}, sensitive)
	})

	t.Run("watsonx missing required field is 400", func(t *testing.T) {
		req := watsonx
		req.Params = map[string]any{"api_key": validWatsonxKey}
		_, err := validateRemoteParams(ctx, provider, req)
		requireStatus(t, err, http.StatusBadRequest, "Parameter validation failed")
	})

	t.Run("no params for a provider with a schema is 400", func(t *testing.T) {
		req := watsonx
		req.Params = nil
		_, err := validateRemoteParams(ctx, provider, req)
		requireStatus(t, err, http.StatusBadRequest, "params is required")
	})

	t.Run("hosted_vllm requires only api_base", func(t *testing.T) {
		req := watsonx
		req.ProviderID = "hosted_vllm"
		req.Params = map[string]any{"api_base": "http://vllm.example.com:8000/v1"}
		_, err := validateRemoteParams(ctx, provider, req)
		require.NoError(t, err)
	})

	t.Run("unknown provider is 404", func(t *testing.T) {
		req := watsonx
		req.ProviderID = "nope"
		_, err := validateRemoteParams(ctx, provider, req)
		requireStatus(t, err, http.StatusNotFound, "not found")
	})
}
