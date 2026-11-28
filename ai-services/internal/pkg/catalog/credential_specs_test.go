package catalog

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestLoadServiceCredentialSpecs_ChatDeclaresItsGrantsInBothRuntimes(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	for _, rt := range []string{"podman", "openshift"} {
		scoped, err := provider.WithRuntime(rt)
		require.NoError(t, err)

		specs, err := scoped.LoadServiceCredentialSpecs("chat")

		require.NoError(t, err, rt)
		require.Len(t, specs, 1, rt)
		assert.Equal(t, "authServiceCredentialSecret", specs[0].ValuesKey, rt)
		assert.Equal(t, map[string][]string{"similarity": {"read"}}, specs[0].Grants, rt)

		// The key is declared in values.yaml, so the deployer finds it in the values.
		values, err := scoped.LoadServiceValues("chat", nil)
		require.NoError(t, err, rt)
		assert.Contains(t, values, specs[0].ValuesKey, rt)
	}
}

func TestLoadServiceCredentialSpecs_OtherBuiltInServicesDeclareNone(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)
	scoped, err := provider.WithRuntime("podman")
	require.NoError(t, err)

	for _, id := range []string{"similarity", "summarize", "digitize"} {
		specs, err := scoped.LoadServiceCredentialSpecs(id)

		require.NoError(t, err, id)
		assert.Empty(t, specs, id)
	}
}

func TestLoadServiceCredentialSpecs_BundleServiceIsRefused(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	bundleFS := fstest.MapFS{
		"evil/podman/values.yaml":  {Data: []byte("# @credential:service grants=similarity:read\nk: \"\"\n")},
		"plain/podman/values.yaml": {Data: []byte("k: \"\"\n")},
	}
	for _, id := range []string{"evil", "plain"} {
		provider.items[id] = &catalogItem{
			Path:    id,
			Service: &types.Service{ID: id, Type: "service"},
			itemFS:  bundleFS,
		}
	}
	scoped, err := provider.WithRuntime("podman")
	require.NoError(t, err)

	_, err = scoped.LoadServiceCredentialSpecs("evil")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "built-in")

	specs, err := scoped.LoadServiceCredentialSpecs("plain")
	require.NoError(t, err)
	assert.Empty(t, specs)
}

func TestLoadServiceCredentialSpecs_BundleCannotShadowChat(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	// A bundle whose catalog id collides with a built-in replaces it (see loadBundleItems).
	// It must not inherit the built-in's credential, nor be able to declare its own.
	provider.items["chat"] = &catalogItem{
		Path:    "chat",
		Service: &types.Service{ID: "chat", Type: "service"},
		itemFS: fstest.MapFS{
			"chat/podman/values.yaml": {Data: []byte("# @credential:service grants=similarity:read\nauthServiceCredentialSecret: \"\"\n")},
		},
	}
	scoped, err := provider.WithRuntime("podman")
	require.NoError(t, err)

	_, err = scoped.LoadServiceCredentialSpecs("chat")

	require.Error(t, err)
}
