package params

import (
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBuilder(t *testing.T) *ParamBuilder {
	t.Helper()

	base, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	b, err := NewParamBuilder(base).WithRuntime("podman")
	require.NoError(t, err)

	return b
}

func TestBuildComponentParams_DefaultProfile(t *testing.T) {
	const granite33 = "ibm-granite/granite-3.3-8b-instruct"

	b := newTestBuilder(t)
	llm := func(params map[string]any) apimodels.Component {
		return apimodels.Component{ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0.0", Params: params}
	}

	t.Run("app deploy merges standard 4-card default profile into values", func(t *testing.T) {
		got, err := b.buildComponentParams(llm(map[string]any{"model": granite33}))
		require.NoError(t, err)

		assert.Equal(t, granite33, got.Values["model"])
		assert.Equal(t, granite33, got.ArgParams["llm.model"])
		assert.Equal(t, "standard", got.Values["profile"])
		assert.Equal(t, 32768, got.Values["maxModelLen"])
		assert.Equal(t, map[string]any{"ibm.com/spyre_pf": 4}, got.Values["resources"].(map[string]any)["accelerators"])
	})

	t.Run("params cannot override profile hardware values", func(t *testing.T) {
		got, err := b.buildComponentParams(llm(map[string]any{"model": granite33, "maxModelLen": 1}))
		require.NoError(t, err)

		assert.Equal(t, 32768, got.Values["maxModelLen"])
	})

	t.Run("component without profiles is unaffected", func(t *testing.T) {
		got, err := b.buildComponentParams(apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch", Version: "1.0.0"})
		require.NoError(t, err)

		assert.NotContains(t, got.Values, "maxModelLen")
	})

	t.Run("request params are not modified", func(t *testing.T) {
		params := map[string]any{"apiKey": "k"}
		_, err := b.buildComponentParams(llm(params))
		require.NoError(t, err)

		assert.Equal(t, map[string]any{"apiKey": "k"}, params)
	})
}
