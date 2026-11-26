package catalog

import (
	"slices"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelProviders(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType constants.DeploymentType
		want           []types.ProviderInfo
	}{
		{
			name:           "local providers are the model components",
			deploymentType: "local",
			want: []types.ProviderInfo{
				{ID: "vllm-cpu", Name: "AI Inference on CPU"},
				{ID: "vllm-spyre", Name: "Red Hat AI Inference with Spyre"},
			},
		},
		{
			name:           "remote providers are the models connectors",
			deploymentType: "remote",
			want: []types.ProviderInfo{
				{ID: "hosted_vllm", Name: "VLLM"},
				{ID: "watsonx", Name: "IBM watsonx.ai on IBM Cloud"},
			},
		},
		{
			name: "no deployment type lists both",
			want: []types.ProviderInfo{
				{ID: "hosted_vllm", Name: "VLLM"},
				{ID: "vllm-cpu", Name: "AI Inference on CPU"},
				{ID: "vllm-spyre", Name: "Red Hat AI Inference with Spyre"},
				{ID: "watsonx", Name: "IBM watsonx.ai on IBM Cloud"},
			},
		},
		{
			name:           "unknown deployment type lists nothing",
			deploymentType: "hybrid",
			want:           []types.ProviderInfo{},
		},
	}

	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ElementsMatch(t, tt.want, provider.GetModelProviders(tt.deploymentType))
		})
	}
}

func TestGetModels(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	models, err := provider.GetModels()
	require.NoError(t, err)

	byID := make(map[string]types.Model, len(models))
	ids := make([]string, 0, len(models))

	for _, m := range models {
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}

	require.Contains(t, byID, "ibm-granite/granite-3.3-8b-instruct")
	assert.Equal(t, "Granite 3.3 8B Instruct", byID["ibm-granite/granite-3.3-8b-instruct"].Name)
	require.Contains(t, byID, "ibm/granite-4-h-small", "remote-only models are listed")
	assert.Equal(t, "Granite 4 H Small", byID["ibm/granite-4-h-small"].Name)

	slices.Sort(ids)
	assert.Equal(t, ids, slices.Compact(slices.Clone(ids)), "each model is listed once, although several providers serve it")
}

func TestGetArchitectureModels_ListsModelsOfModelProviders(t *testing.T) {
	useRuntime(t)

	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	got, err := provider.GetArchitectureModels(t.Context(), "rag")
	require.NoError(t, err)

	assert.Contains(t, got, "ibm-granite/granite-4.1-8b-fp8")
	assert.Contains(t, got, "ibm-granite/granite-embedding-278m-multilingual")
}

func TestGetServiceModels_ExcludesProviders(t *testing.T) {
	useRuntime(t)

	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	all, err := provider.GetServiceModels(t.Context(), "chat")
	require.NoError(t, err)
	require.NotEmpty(t, all)

	none, err := provider.GetServiceModels(t.Context(), "chat", "vllm-spyre", "vllm-cpu")
	require.NoError(t, err)
	assert.Empty(t, none, "models of excluded providers are not listed")
}

// useRuntime sets the runtime factory the model extraction reads, for the length of the test.
func useRuntime(t *testing.T) {
	t.Helper()

	prev := vars.RuntimeFactory
	vars.RuntimeFactory = runtime.NewRuntimeFactory(runtimeTypes.RuntimeTypePodman)
	t.Cleanup(func() { vars.RuntimeFactory = prev })
}
