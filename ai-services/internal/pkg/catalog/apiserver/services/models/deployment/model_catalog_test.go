package deployment

import (
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

func requireStatus(t *testing.T, err error, code int, msgContains string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}

func TestModelCatalog_ComponentTypesComeFromMetadata(t *testing.T) {
	mc := modelCatalog{provider: setupTestCatalogProvider(t)}

	assert.Equal(t, []string{"embedding", "llm", "reranker"}, mc.componentTypes())
	assert.NotContains(t, mc.componentTypes(), "vector_db", "non-model components are excluded")
}

func TestModelCatalog_ValidateType(t *testing.T) {
	mc := modelCatalog{provider: setupTestCatalogProvider(t)}

	require.NoError(t, mc.validateType("llm"))
	requireStatus(t, mc.validateType("vector_db"), http.StatusBadRequest, "component_type")
	requireStatus(t, mc.validateType("database"), http.StatusBadRequest, "must be one of")
}

func TestModelCatalog_ModeFor(t *testing.T) {
	mc := modelCatalog{provider: setupTestCatalogProvider(t)}

	for componentType, want := range map[string]string{"llm": "chat", "embedding": "embedding", "reranker": "rerank"} {
		got, err := mc.modeFor(componentType)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	t.Run("non-model component type is 400", func(t *testing.T) {
		_, err := mc.modeFor("vector_db")
		requireStatus(t, err, http.StatusBadRequest, "component_type")
	})

	t.Run("unknown component type is 400", func(t *testing.T) {
		_, err := mc.modeFor("database")
		requireStatus(t, err, http.StatusBadRequest, "must be one of")
	})
}

func TestModeOf_NoModeDeclaredIsEmptyNotAnError(t *testing.T) {
	comps := []types.Component{
		{ComponentType: "llm", ID: "a", LiteLLM: &types.LiteLLMConfig{}},
		{ComponentType: "llm", ID: "b"},
		{ComponentType: "llm", ID: "c", LiteLLM: &types.LiteLLMConfig{ModelMode: "chat"}},
		{ComponentType: "reranker", ID: "d"},
	}

	assert.Equal(t, "chat", modeOf(comps, "llm"), "skips providers that declare no mode")
	assert.Empty(t, modeOf(comps, "reranker"))
	assert.Empty(t, modeOf(comps, "embedding"))
}
