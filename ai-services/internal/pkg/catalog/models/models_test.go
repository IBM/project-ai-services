package models_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

func TestLoadModelsFromFile(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/p/models.yaml": &fstest.MapFile{Data: []byte(`
- id: org/model-a
  default: false
  title: model-a
  description: |-
    line one

    line two
- id: org/model-b
  default: true
  title: model-b
  description: desc b
`)},
		"llm/bad/models.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}

	t.Run("parses entries", func(t *testing.T) {
		got, err := models.List(fsys, "llm/p")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 models, got %d", len(got))
		}
		if got[0].ID != "org/model-a" || got[0].Description != "line one\n\nline two" {
			t.Errorf("unexpected first model: %+v", got[0])
		}
		if !got[1].Default {
			t.Errorf("expected second model to be default")
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		got, err := models.List(fsys, "llm/none")
		if err != nil || got != nil {
			t.Fatalf("expected nil, nil; got %v, %v", got, err)
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		if _, err := models.List(fsys, "llm/bad"); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestInjectModelsIntoSchema(t *testing.T) {
	newSchema := func() map[string]any {
		return map[string]any{
			"properties": map[string]any{
				"model": map[string]any{"type": "string", "title": "Model"},
			},
		}
	}
	modelField := func(s map[string]any) map[string]any {
		return s["properties"].(map[string]any)["model"].(map[string]any)
	}

	t.Run("uses default flag", func(t *testing.T) {
		s := newSchema()
		models.InjectIntoSchema(s, []types.Model{
			{ID: "a", Title: "A", Description: "da"},
			{ID: "b", Default: true, Title: "B", Description: "db"},
		})
		f := modelField(s)
		if f["default"] != "b" {
			t.Errorf("default = %v, want b", f["default"])
		}
		oneOf := f["oneOf"].([]any)
		if len(oneOf) != 2 || oneOf[0].(map[string]any)["const"] != "a" {
			t.Errorf("unexpected oneOf: %v", oneOf)
		}
		if f["title"] != "Model" {
			t.Errorf("existing field attributes must be preserved")
		}
	})

	t.Run("falls back to first model", func(t *testing.T) {
		s := newSchema()
		models.InjectIntoSchema(s, []types.Model{{ID: "a"}, {ID: "b"}})
		if got := modelField(s)["default"]; got != "a" {
			t.Errorf("default = %v, want a", got)
		}
	})

	t.Run("no models is a no-op", func(t *testing.T) {
		s := newSchema()
		models.InjectIntoSchema(s, nil)
		if _, ok := modelField(s)["oneOf"]; ok {
			t.Error("oneOf must not be set")
		}
	})

	t.Run("schema without model property is a no-op", func(t *testing.T) {
		s := map[string]any{"properties": map[string]any{"apiKey": map[string]any{}}}
		models.InjectIntoSchema(s, []types.Model{{ID: "a"}})
		if _, ok := s["properties"].(map[string]any)["model"]; ok {
			t.Error("model property must not be created")
		}
	})
}

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

	base, err := catalog.NewCatalogProvider(nil)
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
