package profiles

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/p/profiles.yaml": &fstest.MapFile{Data: []byte(`
- id: standard
  default: true
  name: Standard
  description: 4 cards
  params:
    resources:
      memory: 214748364800
      accelerators:
        ibm.com/spyre_pf: 4
`)},
		"llm/bad/profiles.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}

	t.Run("parses entries", func(t *testing.T) {
		got, err := List(fsys, "llm/p")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "standard", got[0].ID)
		assert.True(t, got[0].Default)
		assert.Equal(t, 214748364800, got[0].Params.Resources.Memory)
		assert.Equal(t, 4, got[0].Params.Resources.Accelerators["ibm.com/spyre_pf"])
	})

	t.Run("missing file means no profiles", func(t *testing.T) {
		got, err := List(fsys, "llm/none")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		_, err := List(fsys, "llm/bad")
		assert.Error(t, err)
	})
}

func TestByID(t *testing.T) {
	got := ByID([]Profile{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}})
	assert.Len(t, got, 2)
	assert.Equal(t, "B", got["b"].Name)

	assert.Empty(t, ByID(nil))
}

func TestIsDefault(t *testing.T) {
	tests := []struct {
		name      string
		defaultID string // the model's default_profile
		profile   Profile
		want      bool
	}{
		{"model default matches", "balanced", Profile{ID: "balanced"}, true},
		{"model default overrides profile flag", "balanced", Profile{ID: "standard", Default: true}, false},
		{"no model default: profile flag true", "", Profile{ID: "standard", Default: true}, true},
		{"no model default: profile flag false", "", Profile{ID: "balanced"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsDefault(tt.defaultID, tt.profile))
		})
	}
}
