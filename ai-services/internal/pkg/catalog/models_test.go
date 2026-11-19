package catalog

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/models"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// TestEmbeddedProviderSchemasIncludeModels verifies that, for every runtime, the
// merged schema of each embedded model provider exposes its models from models.yaml.
func TestEmbeddedProviderSchemasIncludeModels(t *testing.T) {
	tests := []struct {
		componentType string
		providerID    string
		wantDefault   string
		wantModels    []string
	}{
		{"llm", "vllm-spyre", "ibm-granite/granite-4.1-8b-fp8", []string{"ibm-granite/granite-3.3-8b-instruct", "ibm-granite/granite-4.1-8b-fp8"}},
		{"llm", "vllm-cpu", "ibm-granite/granite-4.1-8b", []string{"ibm-granite/granite-3.3-8b-instruct", "ibm-granite/granite-4.1-8b"}},
		{"llm", "watsonx", "ibm/granite-4-h-small", []string{"ibm/granite-4-h-small"}},
		{"reranker", "vllm-spyre", "BAAI/bge-reranker-v2-m3", []string{"BAAI/bge-reranker-v2-m3"}},
		{"reranker", "vllm-cpu", "BAAI/bge-reranker-v2-m3", []string{"BAAI/bge-reranker-v2-m3"}},
		{"embedding", "vllm-cpu", "ibm-granite/granite-embedding-278m-multilingual", []string{"ibm-granite/granite-embedding-278m-multilingual"}},
	}

	base, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("failed to create catalog provider: %v", err)
	}

	for _, rt := range []runtimeTypes.RuntimeType{runtimeTypes.RuntimeTypePodman, runtimeTypes.RuntimeTypeOpenShift} {
		provider, err := base.WithRuntime(rt.String())
		if err != nil {
			t.Fatalf("WithRuntime(%s): %v", rt, err)
		}

		for _, tc := range tests {
			t.Run(rt.String()+"/"+tc.componentType+"/"+tc.providerID, func(t *testing.T) {
				schema, err := provider.GetComponentProviderParams(context.Background(), tc.componentType, tc.providerID)
				if err != nil {
					t.Fatalf("GetComponentProviderParams: %v", err)
				}

				got := map[string]bool{}
				models.ExtractFromSchema(schema, got)
				if len(got) != len(tc.wantModels) {
					t.Errorf("models = %v, want %v", got, tc.wantModels)
				}
				for _, m := range tc.wantModels {
					if !got[m] {
						t.Errorf("model %q missing from schema", m)
					}
				}

				field := schema["properties"].(map[string]any)["model"].(map[string]any)
				if field["default"] != tc.wantDefault {
					t.Errorf("default = %v, want %v", field["default"], tc.wantDefault)
				}
			})
		}
	}
}
