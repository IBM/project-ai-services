package profiles

import (
	"testing"
	"testing/fstest"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
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

	t.Run("parses hardware params", func(t *testing.T) {
		withParams := fstest.MapFS{"llm/p/profiles.yaml": &fstest.MapFile{Data: []byte(`
- id: standard
  params:
    maxModelLen: 32768
    maxBatchSize: 32
    maxNumBatchedTokens: 8096
    shmSize: "2Gi"
`)}}

		got, err := List(withParams, "llm/p")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, 32768, got[0].Params.MaxModelLen)
		assert.Equal(t, 32, got[0].Params.MaxBatchSize)
		assert.Equal(t, 8096, got[0].Params.MaxNumBatchedTokens)
		assert.Equal(t, "2Gi", got[0].Params.ShmSize)
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

func TestParamValues(t *testing.T) {
	t.Run("spyre profile sets every key", func(t *testing.T) {
		p := Param{
			Resources:    Resources{Memory: 214748364800, Accelerators: map[string]int{"ibm.com/spyre_pf": 4}},
			MaxModelLen:  32768,
			MaxBatchSize: 32,
			ShmSize:      "2Gi",
		}

		assert.Equal(t, map[string]any{
			"resources": map[string]any{
				"memory":       214748364800,
				"accelerators": map[string]int{"ibm.com/spyre_pf": 4},
			},
			"maxModelLen":  32768,
			"maxBatchSize": 32,
			"shmSize":      "2Gi",
		}, p.Values())
	})

	t.Run("cpu profile omits unset keys", func(t *testing.T) {
		p := Param{Resources: Resources{Memory: 161061273600}, MaxModelLen: 8096, MaxBatchSize: 32, MaxNumBatchedTokens: 8096}

		got := p.Values()
		assert.Equal(t, map[string]any{"memory": 161061273600}, got["resources"])
		assert.Equal(t, 8096, got["maxNumBatchedTokens"])
		assert.NotContains(t, got, "shmSize")
	})

	t.Run("empty param yields no keys", func(t *testing.T) {
		assert.Empty(t, Param{}.Values())
	})
}

func TestResolve(t *testing.T) {
	list := []Profile{
		{ID: "standard", Default: true},
		{ID: "balanced"},
		{ID: "compact-4k"},
	}
	model := &types.Model{ID: "m1", DefaultProfile: "balanced", Profiles: []string{"standard", "balanced"}}

	tests := []struct {
		name      string
		list      []Profile
		model     *types.Model
		requested string
		wantID    string // "" means nil result
		wantErr   bool
	}{
		{"requested wins over model default", list, model, "standard", "standard", false},
		{"model default when nothing requested", list, model, "", "balanced", false},
		{"provider default flag when model has no default", list, &types.Model{ID: "m2", Profiles: []string{"standard"}}, "", "standard", false},
		{"provider default flag when model is nil", list, nil, "", "standard", false},
		{"model default missing from list falls back to flag", list, &types.Model{ID: "m3", DefaultProfile: "gone"}, "", "standard", false},
		{"requested not listed for model", list, model, "compact-4k", "", true},
		{"requested not defined in profiles.yaml", list, &types.Model{ID: "m4", Profiles: []string{"ghost"}}, "ghost", "", true},
		{"requested with nil model only needs to be defined", list, nil, "compact-4k", "compact-4k", false},
		{"requested unknown with nil model", list, nil, "ghost", "", true},
		{"no profiles and nothing requested", nil, model, "", "", false},
		{"no profiles but one requested", nil, nil, "standard", "", true},
		{"no default anywhere", []Profile{{ID: "a"}}, nil, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.list, tt.model, tt.requested)

			if tt.wantErr {
				require.ErrorIs(t, err, ErrNotSupported)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			if tt.wantID == "" {
				assert.Nil(t, got)

				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}

func TestValuesFor(t *testing.T) {
	fsys := fstest.MapFS{
		"llm/spyre/profiles.yaml": &fstest.MapFile{Data: []byte(`
- id: standard
  default: true
  params:
    resources:
      memory: 214748364800
      accelerators:
        ibm.com/spyre_pf: 4
    maxModelLen: 32768
- id: compact-4k
  params:
    maxModelLen: 4096
`)},
		"llm/bad/profiles.yaml": &fstest.MapFile{Data: []byte("not: [a list")},
	}
	model := &types.Model{ID: "m1", DefaultProfile: "standard", Profiles: []string{"standard", "compact-4k"}}

	// Resolve's selection order is covered by TestResolve; this checks the values ValuesFor returns.
	t.Run("returns the values and the ID of the chosen profile", func(t *testing.T) {
		got, err := ValuesFor(fsys, "llm/spyre", model, "")
		require.NoError(t, err)
		assert.Equal(t, "standard", got["profile"])
		assert.Equal(t, 32768, got["maxModelLen"])
		assert.Equal(t, map[string]any{
			"memory":       214748364800,
			"accelerators": map[string]int{"ibm.com/spyre_pf": 4},
		}, got["resources"])

		got, err = ValuesFor(fsys, "llm/spyre", model, "compact-4k")
		require.NoError(t, err)
		assert.Equal(t, "compact-4k", got["profile"])
		assert.Equal(t, 4096, got["maxModelLen"])
	})

	t.Run("provider without profiles.yaml gives no values", func(t *testing.T) {
		got, err := ValuesFor(fsys, "vector_db/opensearch", nil, "")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("unsupported profile", func(t *testing.T) {
		_, err := ValuesFor(fsys, "llm/spyre", &types.Model{ID: "m2", Profiles: []string{"standard"}}, "compact-4k")
		assert.ErrorIs(t, err, ErrNotSupported)
	})

	t.Run("invalid profiles.yaml is a plain error, not ErrNotSupported", func(t *testing.T) {
		_, err := ValuesFor(fsys, "llm/bad", nil, "")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNotSupported)
	})
}
