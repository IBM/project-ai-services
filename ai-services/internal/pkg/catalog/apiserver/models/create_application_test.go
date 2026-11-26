package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComponentValueParams(t *testing.T) {
	t.Run("no model returns params as is", func(t *testing.T) {
		c := Component{Params: map[string]any{"a": 1}}

		assert.Equal(t, map[string]any{"a": 1}, c.ValueParams())
	})

	t.Run("model is added without mutating params", func(t *testing.T) {
		c := Component{Model: "org/m", Params: map[string]any{"a": 1}}

		assert.Equal(t, map[string]any{"a": 1, "model": "org/m"}, c.ValueParams())
		assert.Equal(t, map[string]any{"a": 1}, c.Params)
	})

	t.Run("model without params", func(t *testing.T) {
		assert.Equal(t, map[string]any{"model": "org/m"}, Component{Model: "org/m"}.ValueParams())
	})
}

func TestComponentIdentityParams(t *testing.T) {
	t.Run("without instance equals value params", func(t *testing.T) {
		c := Component{Model: "org/m"}

		assert.Equal(t, c.ValueParams(), c.IdentityParams())
	})

	t.Run("instance id distinguishes the entry", func(t *testing.T) {
		a := Component{InstanceID: "id-a"}
		b := Component{InstanceID: "id-b"}

		assert.Equal(t, map[string]any{"instance_id": "id-a"}, a.IdentityParams())
		assert.NotEqual(t, a.IdentityParams(), b.IdentityParams())
		assert.Empty(t, a.ValueParams())
	})
}
