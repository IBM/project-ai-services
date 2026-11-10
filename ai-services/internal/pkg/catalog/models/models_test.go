package models

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/profiles"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestList(t *testing.T) {
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
		got, err := List(fsys, "llm/p")
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
		got, err := List(fsys, "llm/none")
		if err != nil || got != nil {
			t.Fatalf("expected nil, nil; got %v, %v", got, err)
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		if _, err := List(fsys, "llm/bad"); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestFind(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/p/models.yaml":   &fstest.MapFile{Data: []byte("- id: a\n- id: b\n  title: B\n")},
		"llm/bad/models.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}

	t.Run("found", func(t *testing.T) {
		m, err := Find(fsys, "llm/p", "b")
		if err != nil || m == nil || m.Title != "B" {
			t.Fatalf("got %+v, %v", m, err)
		}
	})

	t.Run("not listed", func(t *testing.T) {
		if m, err := Find(fsys, "llm/p", "zzz"); err != nil || m != nil {
			t.Fatalf("expected nil, nil; got %+v, %v", m, err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if m, err := Find(fsys, "llm/none", "a"); err != nil || m != nil {
			t.Fatalf("expected nil, nil; got %+v, %v", m, err)
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		if _, err := Find(fsys, "llm/bad", "a"); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestInjectIntoSchema(t *testing.T) {
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
		InjectIntoSchema(s, []types.Model{
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
		InjectIntoSchema(s, []types.Model{{ID: "a"}, {ID: "b"}})
		if got := modelField(s)["default"]; got != "a" {
			t.Errorf("default = %v, want a", got)
		}
	})

	t.Run("no models is a no-op", func(t *testing.T) {
		s := newSchema()
		InjectIntoSchema(s, nil)
		if _, ok := modelField(s)["oneOf"]; ok {
			t.Error("oneOf must not be set")
		}
	})

	t.Run("schema without model property is a no-op", func(t *testing.T) {
		s := map[string]any{"properties": map[string]any{"apiKey": map[string]any{}}}
		InjectIntoSchema(s, []types.Model{{ID: "a"}})
		if _, ok := s["properties"].(map[string]any)["model"]; ok {
			t.Error("model property must not be created")
		}
	})
}

func TestExtractFromSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   map[string]bool
	}{
		{
			name: "collects const values from model properties",
			schema: map[string]any{"properties": map[string]any{
				"model": map[string]any{"oneOf": []any{
					map[string]any{"const": "a"},
					map[string]any{"const": "b"},
				}},
				"embedding_model": map[string]any{"oneOf": []any{map[string]any{"const": "c"}}},
			}},
			want: map[string]bool{"a": true, "b": true, "c": true},
		},
		{
			name: "ignores non-model keys, empty and non-string consts",
			schema: map[string]any{"properties": map[string]any{
				"apiKey": map[string]any{"oneOf": []any{map[string]any{"const": "x"}}},
				"model": map[string]any{"oneOf": []any{
					map[string]any{"const": ""},
					map[string]any{"const": 1},
					"not-a-map",
				}},
			}},
			want: map[string]bool{},
		},
		{
			name:   "no properties",
			schema: map[string]any{},
			want:   map[string]bool{},
		},
		{
			name:   "model property without oneOf",
			schema: map[string]any{"properties": map[string]any{"model": map[string]any{"type": "string"}}},
			want:   map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]bool{}
			ExtractFromSchema(tt.schema, got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetInfo(t *testing.T) {
	t.Run("sets name and type id when empty", func(t *testing.T) {
		r := &types.ModelDeployOptions{}
		SetInfo(r, "Granite", "llm")
		if r.Name != "Granite" || r.ComponentType.ID != "llm" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("first provider wins", func(t *testing.T) {
		r := &types.ModelDeployOptions{}
		SetInfo(r, "Granite", "llm")
		SetInfo(r, "Other", "embedding")
		if r.Name != "Granite" || r.ComponentType.ID != "llm" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("fills only the missing field", func(t *testing.T) {
		r := &types.ModelDeployOptions{Name: "Preset"}
		SetInfo(r, "Granite", "llm")
		if r.Name != "Preset" || r.ComponentType.ID != "llm" {
			t.Errorf("got %+v", r)
		}
	})
}

func TestSetComponentTypeNames(t *testing.T) {
	components := []types.Component{
		{ComponentType: "embedding", ComponentName: "Embedding", ComponentShortName: "EMB"},
		{ComponentType: "llm", ComponentName: "Large language model", ComponentShortName: "LLM"},
		{ComponentType: "llm", ComponentName: "Second", ComponentShortName: "2nd"},
	}

	t.Run("fills names from first component of matching type", func(t *testing.T) {
		r := &types.ModelDeployOptions{ComponentType: types.ComponentType{ID: "llm"}}
		SetComponentTypeNames(r, components)
		if r.ComponentType.Name != "Large language model" || r.ComponentType.ShortName != "LLM" {
			t.Errorf("got %+v", r.ComponentType)
		}
	})

	t.Run("keeps existing names", func(t *testing.T) {
		r := &types.ModelDeployOptions{ComponentType: types.ComponentType{ID: "llm", Name: "Keep", ShortName: "K"}}
		SetComponentTypeNames(r, components)
		if r.ComponentType.Name != "Keep" || r.ComponentType.ShortName != "K" {
			t.Errorf("got %+v", r.ComponentType)
		}
	})

	t.Run("no matching component leaves names empty", func(t *testing.T) {
		r := &types.ModelDeployOptions{ComponentType: types.ComponentType{ID: "reranker"}}
		SetComponentTypeNames(r, components)
		if r.ComponentType.Name != "" || r.ComponentType.ShortName != "" {
			t.Errorf("got %+v", r.ComponentType)
		}
	})
}

func TestBuildDeployProfiles(t *testing.T) {
	byID := map[string]profiles.Profile{
		"p1": {ID: "p1", Name: "P1", Description: "d1", Default: true, Params: profiles.Param{
			Resources: profiles.Resources{Memory: 100, Accelerators: map[string]int{"ibm.com/spyre_pf": 4}},
		}},
		"p2": {ID: "p2", Name: "P2", Params: profiles.Param{Resources: profiles.Resources{Memory: 200}}},
	}

	t.Run("maps profiles in model order with cpu and storage", func(t *testing.T) {
		got, err := BuildDeployProfiles(&types.Model{ID: "m", Profiles: []string{"p2", "p1"}}, byID, "prov", types.Resources{CPU: 2, Storage: 10})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0].ID != "p2" || got[1].ID != "p1" {
			t.Fatalf("unexpected order: %+v", got)
		}
		want := types.Resources{CPU: 2, Memory: 100, Storage: 10, Accelerators: map[string]int{"ibm.com/spyre_pf": 4}}
		if !reflect.DeepEqual(got[1].Resources, want) {
			t.Errorf("resources = %+v, want %+v", got[1].Resources, want)
		}
		if got[1].Name != "P1" || got[1].Description != "d1" {
			t.Errorf("unexpected metadata: %+v", got[1])
		}
	})

	t.Run("profile default flag used when model has no default_profile", func(t *testing.T) {
		got, _ := BuildDeployProfiles(&types.Model{ID: "m", Profiles: []string{"p1", "p2"}}, byID, "prov", types.Resources{})
		if !got[0].Default || got[1].Default {
			t.Errorf("defaults = %v, %v; want true, false", got[0].Default, got[1].Default)
		}
	})

	t.Run("model default_profile overrides profile flag", func(t *testing.T) {
		got, _ := BuildDeployProfiles(&types.Model{ID: "m", DefaultProfile: "p2", Profiles: []string{"p1", "p2"}}, byID, "prov", types.Resources{})
		if got[0].Default || !got[1].Default {
			t.Errorf("defaults = %v, %v; want false, true", got[0].Default, got[1].Default)
		}
	})

	t.Run("unknown profile id is an error", func(t *testing.T) {
		if _, err := BuildDeployProfiles(&types.Model{ID: "m", Profiles: []string{"nope"}}, byID, "prov", types.Resources{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("no profiles gives empty non-nil slice", func(t *testing.T) {
		got, err := BuildDeployProfiles(&types.Model{ID: "m"}, byID, "prov", types.Resources{})
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v", got, err)
		}
	})
}

func TestFilterAndSortProviders(t *testing.T) {
	local, remote := constants.DeploymentTypeLocal, constants.DeploymentTypeRemote
	providers := []types.ModelDeployProvider{
		{ID: "watsonx", DeploymentType: remote},
		{ID: "vllm-spyre", DeploymentType: local},
		{ID: "vllm-cpu", DeploymentType: local, Default: true},
		{ID: "a-remote", DeploymentType: remote},
		{ID: "b-local", DeploymentType: local},
	}

	ids := func(ps []types.ModelDeployProvider) []string {
		out := make([]string, 0, len(ps))
		for _, p := range ps {
			out = append(out, p.ID)
		}

		return out
	}

	t.Run("local first, default first, then by id", func(t *testing.T) {
		got := ids(FilterAndSortProviders(providers, ""))
		want := []string{"vllm-cpu", "b-local", "vllm-spyre", "a-remote", "watsonx"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("filters by provider id", func(t *testing.T) {
		got := ids(FilterAndSortProviders(providers, "watsonx"))
		if !reflect.DeepEqual(got, []string{"watsonx"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("unknown provider gives empty non-nil slice", func(t *testing.T) {
		got := FilterAndSortProviders(providers, "nope")
		if got == nil || len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("does not mutate input order", func(t *testing.T) {
		if providers[0].ID != "watsonx" {
			t.Errorf("input was reordered: %v", ids(providers))
		}
	})
}
