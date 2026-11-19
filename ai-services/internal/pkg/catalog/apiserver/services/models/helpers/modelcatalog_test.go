package helpers

import (
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestModelCatalog(t *testing.T) ModelCatalog {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return New(provider)
}

func requireStatus(t *testing.T, err error, code int, msgContains string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}

func TestModelCatalog_ComponentTypesComeFromMetadata(t *testing.T) {
	mc := newTestModelCatalog(t)

	assert.Equal(t, []string{"embedding", "llm", "reranker"}, mc.ComponentTypes())
	assert.NotContains(t, mc.ComponentTypes(), "vector_db", "non-model components are excluded")
}

func TestModelCatalog_ValidateType(t *testing.T) {
	mc := newTestModelCatalog(t)

	require.NoError(t, mc.ValidateType("llm"))
	requireStatus(t, mc.ValidateType("vector_db"), http.StatusBadRequest, "component_type")
	requireStatus(t, mc.ValidateType("database"), http.StatusBadRequest, "must be one of")
}

func TestModelCatalog_ModeFor(t *testing.T) {
	mc := newTestModelCatalog(t)

	for componentType, want := range map[string]string{"llm": "chat", "embedding": "embedding", "reranker": "rerank"} {
		got, err := mc.ModeFor(componentType)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	t.Run("non-model component type is 400", func(t *testing.T) {
		_, err := mc.ModeFor("vector_db")
		requireStatus(t, err, http.StatusBadRequest, "component_type")
	})

	t.Run("unknown component type is 400", func(t *testing.T) {
		_, err := mc.ModeFor("database")
		requireStatus(t, err, http.StatusBadRequest, "must be one of")
	})
}

func TestModeOf_NoModeDeclaredIsEmptyNotAnError(t *testing.T) {
	comps := []types.Component{
		{ComponentType: types.ComponentType{ID: "llm"}, ID: "a", LiteLLM: &types.LiteLLMConfig{}},
		{ComponentType: types.ComponentType{ID: "llm"}, ID: "b"},
		{ComponentType: types.ComponentType{ID: "llm"}, ID: "c", LiteLLM: &types.LiteLLMConfig{ModelMode: "chat"}},
		{ComponentType: types.ComponentType{ID: "reranker"}, ID: "d"},
	}

	assert.Equal(t, "chat", modeOf(comps, "llm"), "skips providers that declare no mode")
	assert.Empty(t, modeOf(comps, "reranker"))
	assert.Empty(t, modeOf(comps, "embedding"))
}
