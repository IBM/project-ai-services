package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestCleanDescription(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain description unchanged",
			input: "API key for accessing watsonx",
			want:  "API key for accessing watsonx",
		},
		{
			name:  "newlines collapsed to spaces",
			input: "First line\n\nSecond line",
			want:  "First line Second line",
		},
		{
			name:  "markdown bold stripped",
			input: "**Model strengths**: Improved instruction following",
			want:  "Model strengths: Improved instruction following",
		},
		{
			name:  "markdown inline link kept as-is",
			input: "Find your ID on the site\n\n[Open project](https://dataplatform.cloud.ibm.com/projects/?context=wx)",
			want:  "Find your ID on the site [Open project](https://dataplatform.cloud.ibm.com/projects/?context=wx)",
		},
		{
			name: "real watsonxProjectId description",
			input: "Find your ID on the watsonx cloud site when you create a project.\n\n" +
				"[Open project](https://dataplatform.cloud.ibm.com/projects/?context=wx)",
			want: "Find your ID on the watsonx cloud site when you create a project. [Open project](https://dataplatform.cloud.ibm.com/projects/?context=wx)",
		},
		{
			name: "real vllm model description",
			input: "An 8b-parameter instruction-tuned LLM used for natural language understanding and generation tasks.\n\n" +
				"**Model strengths**: Reliable instruction-following, natural conversation, strong reasoning.\n\n" +
				"**Supported Languages**: English (primary), Spanish, French.",
			want: "An 8b-parameter instruction-tuned LLM used for natural language understanding and generation tasks. " +
				"Model strengths: Reliable instruction-following, natural conversation, strong reasoning. " +
				"Supported Languages: English (primary), Spanish, French.",
		},
		{
			name:  "non-string input returns empty",
			input: 42,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanDescription(tt.input)
			if got != tt.want {
				t.Errorf("cleanDescription(%q)\n got:  %q\n want: %q", tt.input, got, tt.want)
			}
		})
	}
}

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
