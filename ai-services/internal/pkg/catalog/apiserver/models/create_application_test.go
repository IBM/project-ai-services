package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponent_EffectiveModel(t *testing.T) {
	tests := []struct {
		name string
		comp Component
		want string
	}{
		{"top-level model", Component{Model: "m-top"}, "m-top"},
		{"legacy params.model", Component{Params: map[string]any{"model": "m-params"}}, "m-params"},
		{"top-level wins over params", Component{Model: "m-top", Params: map[string]any{"model": "m-params"}}, "m-top"},
		{"non-string params.model is ignored", Component{Params: map[string]any{"model": 42}}, ""},
		{"none", Component{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.comp.EffectiveModel())
		})
	}
}

func TestComponent_ParamsWithModel(t *testing.T) {
	t.Run("top-level model is added and wins over params.model", func(t *testing.T) {
		c := Component{Model: "m-top", Params: map[string]any{"model": "m-params", "apiKey": "k"}}

		assert.Equal(t, map[string]any{"model": "m-top", "apiKey": "k"}, c.ParamsWithModel())
		assert.Equal(t, map[string]any{"model": "m-top"}, Component{Model: "m-top"}.ParamsWithModel(), "nil params")
	})

	t.Run("legacy and top-level forms are equivalent", func(t *testing.T) {
		legacy := Component{Params: map[string]any{"model": "m1"}}

		assert.Equal(t, legacy.ParamsWithModel(), Component{Model: "m1"}.ParamsWithModel())
	})

	t.Run("without a model the params are returned unchanged", func(t *testing.T) {
		assert.Equal(t, map[string]any{"apiKey": "k"}, Component{Params: map[string]any{"apiKey": "k"}}.ParamsWithModel())
		assert.Nil(t, Component{}.ParamsWithModel())
	})

	t.Run("request params are not modified", func(t *testing.T) {
		params := map[string]any{"apiKey": "k"}
		_ = Component{Model: "m1", Params: params}.ParamsWithModel()

		assert.Equal(t, map[string]any{"apiKey": "k"}, params)
	})
}

// The request has a top-level model but deliberately no profile: the backend chooses it.
func TestComponent_JSON(t *testing.T) {
	var c Component
	require.NoError(t, json.Unmarshal([]byte(
		`{"component_type":"llm","provider_id":"vllm-spyre","version":"1.0.0","model":"m1","profile":"compact-4k","params":{"apiKey":"k"}}`), &c))

	assert.Equal(t, "m1", c.Model)
	assert.Equal(t, map[string]any{"apiKey": "k"}, c.Params)

	out, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "profile", "profile must not be accepted from the client")
}
