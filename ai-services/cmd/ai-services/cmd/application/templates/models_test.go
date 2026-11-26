package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestModelComponentLines(t *testing.T) {
	comp := catalogTypes.DeployOptionsComponent{
		Type: "llm",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-spyre", Recommendations: []catalogTypes.ModelRecommendation{{Model: "b"}, {Model: "a"}}},
			{ID: "vllm-cpu"},
		},
	}
	entries := []catalogTypes.ModelCatalogEntry{
		{ID: "z", Providers: []catalogTypes.ModelCatalogProvider{
			{ID: "vllm-cpu", DeploymentType: catalogConstants.DeploymentTypeLocal},
			{ID: "vllm-spyre", DeploymentType: catalogConstants.DeploymentTypeLocal, DefaultModel: true},
		}},
		{ID: "m", Providers: []catalogTypes.ModelCatalogProvider{{ID: "vllm-spyre", DeploymentType: catalogConstants.DeploymentTypeLocal}}},
		{ID: "remote-only", Providers: []catalogTypes.ModelCatalogProvider{{ID: "watsonx", DeploymentType: catalogConstants.DeploymentTypeRemote}}},
	}

	t.Run("recommendations, then local models sorted by id", func(t *testing.T) {
		assert.Equal(t, []string{
			"  llm models (choose with --model llm=<model>, the provider with --params llm.<provider>=true):",
			"    recommended by 'chat' for provider 'vllm-spyre': b (primary), a",
			"    available models:",
			"      - m [vllm-spyre]",
			"      - z [vllm-cpu, vllm-spyre (default)]",
		}, modelComponentLines("chat", comp, entries))
	})

	t.Run("no deployable models", func(t *testing.T) {
		lines := modelComponentLines("chat", comp, entries[2:])
		assert.Equal(t, "    no deployable models", lines[len(lines)-1])
	})
}
