package catalog

import (
	"context"
	"testing"

	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// TestEmbeddedProviderSchemasAreEmpty verifies that, for every runtime, the schema of each embedded
// model provider has no properties: models are chosen from models.yaml, not from the schema.
func TestEmbeddedProviderSchemasAreEmpty(t *testing.T) {
	tests := []struct {
		componentType string
		providerID    string
	}{
		{"llm", "vllm-spyre"},
		{"llm", "vllm-cpu"},
		{"reranker", "vllm-spyre"},
		{"reranker", "vllm-cpu"},
		{"embedding", "vllm-cpu"},
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

				if hasNonEmptyProperties(schema) {
					t.Errorf("schema properties = %v, want none", schema["properties"])
				}
			})
		}
	}
}
