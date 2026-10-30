package catalog

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"go.yaml.in/yaml/v3"
)

func TestLoadModelsFromFile(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/p/models.yaml": &fstest.MapFile{Data: []byte(`
- id: org/model-a
  name: model-a
  description: desc a
  license: Apache-2.0
  certified_by: IBM
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values: [s1, s2]
  default_profile: standard
  profiles: [standard, compact-4k]
- id: org/model-b
  default: true
  name: model-b
  description: |-
    line one

    line two
`)},
		"llm/bad/models.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}

	t.Run("parses entries", func(t *testing.T) {
		models, err := loadModelsFromFile(fsys, "llm/p")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}
		a := models[0]
		if a.Name != "model-a" || a.License != "Apache-2.0" || a.CertifiedBy != "IBM" ||
			a.About == nil ||
			a.DefaultProfile != "standard" || len(a.Profiles) != 2 {
			t.Errorf("unexpected first model: %+v", a)
		}
		b := models[1]
		if !b.Default || b.Name != "model-b" || b.Description != "line one\n\nline two" {
			t.Errorf("unexpected second model: %+v", b)
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		models, err := loadModelsFromFile(fsys, "llm/none")
		if err != nil || models != nil {
			t.Fatalf("expected nil, nil; got %v, %v", models, err)
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		if _, err := loadModelsFromFile(fsys, "llm/bad"); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestModelDisplayName(t *testing.T) {
	cases := []struct {
		m    types.Model
		want string
	}{
		{types.Model{ID: "id", Name: "n"}, "n"},
		{types.Model{ID: "id"}, "id"},
	}
	for _, c := range cases {
		if got := modelDisplayName(c.m); got != c.want {
			t.Errorf("modelDisplayName(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestModelSchemaDescription(t *testing.T) {
	load := func(t *testing.T, doc string) types.Model {
		t.Helper()
		var models []types.Model
		if err := yaml.Unmarshal([]byte(doc), &models); err != nil || len(models) != 1 {
			t.Fatalf("unmarshal: %v (%d models)", err, len(models))
		}

		return models[0]
	}

	t.Run("renders about sections as bold-headed paragraphs", func(t *testing.T) {
		m := load(t, `
- id: x
  description: Intro.
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values: [a, b, c]
        - title: "Use cases"
          values: [u1]
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values: [English, French]
`)
		want := "Intro.\n\n**Strengths**: a, b, c.\n\n**Use cases**: u1.\n\n**Languages**: English, French."
		if got := modelSchemaDescription(m); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("single value sections", func(t *testing.T) {
		m := load(t, `
- id: x
  description: Intro.
  about:
    - title: g
      sections:
        - title: Version
          value: "1.0"
`)
		if got, want := modelSchemaDescription(m), "Intro.\n\n**Version**: 1.0."; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("description only", func(t *testing.T) {
		if got := modelSchemaDescription(types.Model{Description: "as is\n\nkept"}); got != "as is\n\nkept" {
			t.Errorf("got %q", got)
		}
	})
}

func TestInjectModelsIntoSchema(t *testing.T) {
	modelField := func(s map[string]any) map[string]any {
		return s["properties"].(map[string]any)["model"].(map[string]any)
	}

	t.Run("creates the model property when the schema omits it", func(t *testing.T) {
		s := map[string]any{"properties": map[string]any{"apiKey": map[string]any{}}}
		injectModelsIntoSchema(s, []types.Model{
			{ID: "a", Name: "A", Description: "da"},
			{ID: "b", Name: "B", Description: "db", Default: true},
		}, "Large language model (LLM)")

		f := modelField(s)
		if f["type"] != "string" || f["title"] != "Large language model (LLM)" || f["default"] != "b" {
			t.Errorf("unexpected model field: %v", f)
		}
		oneOf := f["oneOf"].([]any)
		if len(oneOf) != 2 || oneOf[0].(map[string]any)["const"] != "a" || oneOf[0].(map[string]any)["title"] != "A" {
			t.Errorf("unexpected oneOf: %v", oneOf)
		}
		if _, ok := s["properties"].(map[string]any)["apiKey"]; !ok {
			t.Error("other properties must be preserved")
		}
	})

	t.Run("creates properties when the schema has none", func(t *testing.T) {
		s := map[string]any{}
		injectModelsIntoSchema(s, []types.Model{{ID: "a"}}, "Model")
		if modelField(s)["default"] != "a" {
			t.Errorf("unexpected schema: %v", s)
		}
	})

	t.Run("keeps attributes of an existing model property", func(t *testing.T) {
		s := map[string]any{"properties": map[string]any{
			"model": map[string]any{"type": "string", "title": "Custom", "description": "Custom desc"},
		}}
		injectModelsIntoSchema(s, []types.Model{{ID: "a"}}, "Fallback")
		f := modelField(s)
		if f["title"] != "Custom" || f["description"] != "Custom desc" {
			t.Errorf("existing attributes must be preserved: %v", f)
		}
		if f["oneOf"] == nil {
			t.Error("oneOf must be injected")
		}
	})

	t.Run("falls back to first model as default", func(t *testing.T) {
		s := map[string]any{}
		injectModelsIntoSchema(s, []types.Model{{ID: "a"}, {ID: "b"}}, "Model")
		if got := modelField(s)["default"]; got != "a" {
			t.Errorf("default = %v, want a", got)
		}
	})

	t.Run("no models is a no-op", func(t *testing.T) {
		s := map[string]any{"properties": map[string]any{}}
		injectModelsIntoSchema(s, nil, "Model")
		if _, ok := s["properties"].(map[string]any)["model"]; ok {
			t.Error("model property must not be created")
		}
	})
}

var embeddedModelProviders = [][2]string{
	{"llm", "vllm-spyre"},
	{"llm", "vllm-cpu"},
	{"llm", "watsonx"},
	{"reranker", "vllm-spyre"},
	{"reranker", "vllm-cpu"},
	{"embedding", "vllm-cpu"},
}

// TestEmbeddedModelDescriptionsAreUIParsable checks that every served option keeps the
// bold-headed sections the catalog UI looks for (strengths and languages).
func TestEmbeddedModelDescriptionsAreUIParsable(t *testing.T) {
	base, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("failed to create catalog provider: %v", err)
	}
	provider, err := base.WithRuntime(runtimeTypes.RuntimeTypePodman.String())
	if err != nil {
		t.Fatal(err)
	}

	for _, pr := range embeddedModelProviders {
		schema, err := provider.GetComponentProviderParams(context.Background(), pr[0], pr[1])
		if err != nil {
			t.Fatal(err)
		}
		options := schema["properties"].(map[string]any)["model"].(map[string]any)["oneOf"].([]any)
		for _, o := range options {
			opt := o.(map[string]any)
			desc := strings.ToLower(opt["description"].(string))
			for _, heading := range []string{"**strengths**", "**languages**"} {
				if !strings.Contains(desc, heading) {
					t.Errorf("%s/%s %v: description lacks %s", pr[0], pr[1], opt["const"], heading)
				}
			}
			if opt["title"] == "" {
				t.Errorf("%s/%s %v: empty title", pr[0], pr[1], opt["const"])
			}
		}
	}
}

// TestEmbeddedModelProvidersExposeSchema ensures providers whose only schema field is
// the model still produce a non-empty schema (needed to advertise a schema URL).
func TestEmbeddedModelProvidersExposeSchema(t *testing.T) {
	base, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("failed to create catalog provider: %v", err)
	}

	for _, rt := range []runtimeTypes.RuntimeType{runtimeTypes.RuntimeTypePodman, runtimeTypes.RuntimeTypeOpenShift} {
		provider, err := base.WithRuntime(rt.String())
		if err != nil {
			t.Fatal(err)
		}
		for _, pr := range embeddedModelProviders {
			schema, err := provider.GetComponentProviderParams(context.Background(), pr[0], pr[1])
			if err != nil {
				t.Fatalf("%s/%s/%s: %v", rt, pr[0], pr[1], err)
			}
			if !hasNonEmptyProperties(schema) {
				t.Errorf("%s/%s/%s: schema has no properties", rt, pr[0], pr[1])
			}
			models := map[string]bool{}
			extractModelsFromSchema(schema, models)
			if len(models) == 0 {
				t.Errorf("%s/%s/%s: no models extracted", rt, pr[0], pr[1])
			}
		}
	}
}
