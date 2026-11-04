package catalog

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPodmanProvider returns a catalog provider scoped to the podman runtime.
func newPodmanProvider(t *testing.T) *CatalogProvider {
	t.Helper()
	base, err := NewCatalogProvider(nil)
	require.NoError(t, err)
	p, err := base.WithRuntime("podman")
	require.NoError(t, err)

	return p
}

func TestLoadProfilesFromFile(t *testing.T) {
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
		profiles, err := loadYAMLList[types.Profile](fsys, "llm/p", profilesFileName)
		require.NoError(t, err)
		require.Len(t, profiles, 1)
		assert.Equal(t, "standard", profiles[0].ID)
		assert.True(t, profiles[0].Default)
		assert.Equal(t, 214748364800, profiles[0].Params.Resources.Memory)
		assert.Equal(t, 4, profiles[0].Params.Resources.Accelerators["ibm.com/spyre_pf"])
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		profiles, err := loadYAMLList[types.Profile](fsys, "llm/none", profilesFileName)
		assert.NoError(t, err)
		assert.Nil(t, profiles)
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		_, err := loadYAMLList[types.Profile](fsys, "llm/bad", profilesFileName)
		assert.Error(t, err)
	})
}

func TestGetModelDeployOptions_UnknownModel(t *testing.T) {
	_, err := newPodmanProvider(t).GetModelDeployOptions(context.Background(), "no/such-model", "")
	assert.True(t, errors.Is(err, ErrModelNotFound))
}

// granite-3.3 is served by vllm-spyre (3 profiles), vllm-cpu (1 profile) and the
// hosted_vllm connector (remote). Local providers must come before remote ones.
func TestGetModelDeployOptions_LocalAndRemote(t *testing.T) {
	opts, err := newPodmanProvider(t).GetModelDeployOptions(context.Background(), "ibm-granite/granite-3.3-8b-instruct", "")
	require.NoError(t, err)

	assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", opts.ID)
	assert.Equal(t, "Granite 3.3 8B Instruct", opts.Name)
	assert.Equal(t, "llm", opts.ComponentType)

	ids := make([]string, 0, len(opts.Providers))
	for _, pr := range opts.Providers {
		ids = append(ids, pr.ID)
	}
	// local first (default provider vllm-spyre before vllm-cpu), then remote.
	assert.Equal(t, []string{"vllm-spyre", "vllm-cpu", "hosted_vllm"}, ids)

	spyre := opts.Providers[0]
	assert.Equal(t, "local", spyre.DeploymentType)
	assert.True(t, spyre.Default)
	assert.Contains(t, spyre.Schema, "/api/v1/components/llm/providers/vllm-spyre/params?runtime=podman")
	require.Len(t, spyre.Profiles, 3)
	assert.Equal(t, "standard", spyre.Profiles[0].ID)
	assert.True(t, spyre.Profiles[0].Default)
	assert.Equal(t, 4, spyre.Profiles[0].Resources.Accelerators["ibm.com/spyre_pf"])
	assert.Equal(t, 214748364800, spyre.Profiles[0].Resources.Memory)
	assert.Equal(t, "compact-4k", spyre.Profiles[2].ID)
	assert.False(t, spyre.Profiles[2].Default)
	assert.Equal(t, 1, spyre.Profiles[2].Resources.Accelerators["ibm.com/spyre_pf"])

	remote := opts.Providers[2]
	assert.Equal(t, "remote", remote.DeploymentType)
	assert.Empty(t, remote.Profiles)
	assert.Equal(t, "/api/v1/connectors/models/providers/hosted_vllm/params", remote.Schema)
}

func TestGetModelDeployOptions_ProviderFilter(t *testing.T) {
	opts, err := newPodmanProvider(t).GetModelDeployOptions(context.Background(), "ibm-granite/granite-3.3-8b-instruct", "vllm-cpu")
	require.NoError(t, err)
	require.Len(t, opts.Providers, 1)
	assert.Equal(t, "vllm-cpu", opts.Providers[0].ID)
}

// A known model with a provider filter that matches nothing is an empty list, not a 404.
func TestGetModelDeployOptions_ProviderFilterNoMatch(t *testing.T) {
	opts, err := newPodmanProvider(t).GetModelDeployOptions(context.Background(), "ibm-granite/granite-3.3-8b-instruct", "watsonx")
	require.NoError(t, err)
	assert.Empty(t, opts.Providers)
}

// Reranker and embedding models are served by components that have no values schema properties.
func TestGetModelDeployOptions_Embedding(t *testing.T) {
	opts, err := newPodmanProvider(t).GetModelDeployOptions(context.Background(), "ibm-granite/granite-embedding-278m-multilingual", "")
	require.NoError(t, err)
	assert.Equal(t, "embedding", opts.ComponentType)
	require.NotEmpty(t, opts.Providers)
	assert.Equal(t, "vllm-cpu", opts.Providers[0].ID)
	require.Len(t, opts.Providers[0].Profiles, 1)
	assert.Equal(t, "standard", opts.Providers[0].Profiles[0].ID)
}
