package catalog

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findDeployProvider returns the provider with the given id from the component of the given type.
func findDeployProvider(t *testing.T, components []types.DeployOptionsComponent, componentType, providerID string) types.DeployOptionsProvider {
	t.Helper()

	for _, c := range components {
		if c.Type != componentType {
			continue
		}
		for _, pr := range c.Providers {
			if pr.ID == providerID {
				return pr
			}
		}
	}
	require.FailNow(t, "provider not found", "%s/%s", componentType, providerID)

	return types.DeployOptionsProvider{}
}

func TestGetServiceDeployOptions_ModelProviders(t *testing.T) {
	opts, err := newPodmanProvider(t).GetServiceDeployOptions(context.Background(), "extract")
	require.NoError(t, err)

	t.Run("component category comes from the provider metadata", func(t *testing.T) {
		require.NotEmpty(t, opts.Components)
		for _, c := range opts.Components {
			assert.Equal(t, "models", c.Category, c.Type)
		}
	})

	t.Run("local provider with recommendations", func(t *testing.T) {
		pr := findDeployProvider(t, opts.Components, "llm", "vllm-spyre")

		require.NotNil(t, pr.Models)
		assert.Equal(t, "/api/v1/catalog/models?component_type=llm", pr.Models.URL)
		require.NotNil(t, pr.Instances)
		assert.Equal(t, "Instances", pr.Instances.Name)
		assert.Equal(t, "/api/v1/models?status=Running&status=connected", pr.Instances.URL)
		assert.Equal(t, []types.ModelRecommendation{
			{Model: "ibm-granite/granite-4.1-8b-fp8", Profile: "compact-4k"},
			{Model: "ibm-granite/granite-3.3-8b-instruct", Profile: "compact-4k"},
		}, pr.Recommendations)
	})

	t.Run("local provider without recommendations", func(t *testing.T) {
		pr := findDeployProvider(t, opts.Components, "llm", "vllm-cpu")

		assert.Empty(t, pr.Recommendations)
	})

	t.Run("remote connectors are not offered", func(t *testing.T) {
		for _, c := range opts.Components {
			for _, pr := range c.Providers {
				assert.NotContains(t, []string{"watsonx", "hosted_vllm"}, pr.ID, c.Type)
			}
		}
	})
}

func TestGetArchitectureDeployOptions_NonModelProvidersUnchanged(t *testing.T) {
	opts, err := newPodmanProvider(t).GetArchitectureDeployOptions(context.Background(), "rag")
	require.NoError(t, err)

	for _, c := range opts.GlobalComponents {
		if c.Type == "vector_store" {
			assert.Empty(t, c.Category)
		}
	}

	pr := findDeployProvider(t, opts.GlobalComponents, "vector_store", "opensearch")
	assert.Nil(t, pr.Models)
	assert.Nil(t, pr.Instances)
	assert.Empty(t, pr.Recommendations)
}
