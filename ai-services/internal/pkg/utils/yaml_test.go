package utils

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadYAMLList(t *testing.T) {
	type item struct {
		ID      string `yaml:"id"`
		Default bool   `yaml:"default"`
	}

	fsys := fstest.MapFS{
		"d/ok.yaml":  &fstest.MapFile{Data: []byte("- id: a\n- id: b\n  default: true\n")},
		"d/bad.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}

	t.Run("parses entries", func(t *testing.T) {
		items, err := LoadYAMLList[item](fsys, "d", "ok.yaml")
		require.NoError(t, err)
		assert.Equal(t, []item{{ID: "a"}, {ID: "b", Default: true}}, items)
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		items, err := LoadYAMLList[item](fsys, "d", "none.yaml")
		assert.NoError(t, err)
		assert.Nil(t, items)
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		_, err := LoadYAMLList[item](fsys, "d", "bad.yaml")
		assert.Error(t, err)
	})
}
