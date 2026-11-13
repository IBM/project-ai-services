package validators

import (
	"context"
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const granite33 = "ibm-granite/granite-3.3-8b-instruct"

func newTestValidator(t *testing.T) *ApplicationValidator {
	t.Helper()

	base, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)
	p, err := base.WithRuntime("podman")
	require.NoError(t, err)

	return NewApplicationValidator(p)
}

func requireStatus(t *testing.T, err error, code int) {
	t.Helper()

	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, code, verr.Code)
}

func TestValidateSingleComponent_Model(t *testing.T) {
	v := newTestValidator(t)
	llm := func(model string, params map[string]any) apimodels.Component {
		return apimodels.Component{ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0.0", Model: model, Params: params}
	}

	t.Run("known top-level model passes", func(t *testing.T) {
		assert.NoError(t, v.ValidateSingleComponent(context.Background(), llm(granite33, nil)))
	})

	t.Run("legacy params.model passes", func(t *testing.T) {
		assert.NoError(t, v.ValidateSingleComponent(context.Background(), llm("", map[string]any{"model": granite33})))
	})

	t.Run("unknown top-level model is a 400", func(t *testing.T) {
		err := v.ValidateSingleComponent(context.Background(), llm("no/such-model", nil))

		requireStatus(t, err, http.StatusBadRequest)
		assert.Contains(t, err.Error(), "no/such-model")
	})

	t.Run("model for a provider without models is a 400", func(t *testing.T) {
		err := v.ValidateSingleComponent(context.Background(), apimodels.Component{
			ComponentType: "vector_store", ProviderID: "opensearch", Version: "1.0.0", Model: granite33,
		})

		requireStatus(t, err, http.StatusBadRequest)
	})

	t.Run("unknown provider is still a 404", func(t *testing.T) {
		err := v.ValidateSingleComponent(context.Background(), apimodels.Component{
			ComponentType: "llm", ProviderID: "no-such-provider", Version: "1.0.0", Model: granite33,
		})

		requireStatus(t, err, http.StatusNotFound)
	})
}

func TestCheckComponentConsistency_Model(t *testing.T) {
	svc := func(model string, params map[string]any) apimodels.Service {
		return apimodels.Service{Components: []apimodels.Component{
			{ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0.0", Model: model, Params: params},
		}}
	}

	t.Run("different top-level models across services are rejected, naming the model", func(t *testing.T) {
		seen := map[string]seenComponent{}
		require.NoError(t, checkComponentConsistency("chat", svc("m-a", nil), seen))

		err := checkComponentConsistency("summarize", svc("m-b", nil), seen)

		requireStatus(t, err, http.StatusBadRequest)
		assert.Contains(t, err.Error(), "'model'")
	})

	t.Run("same top-level model across services is accepted", func(t *testing.T) {
		seen := map[string]seenComponent{}
		require.NoError(t, checkComponentConsistency("chat", svc("m-a", nil), seen))

		assert.NoError(t, checkComponentConsistency("summarize", svc("m-a", nil), seen))
	})

	t.Run("top-level and legacy params.model for the same model are accepted", func(t *testing.T) {
		seen := map[string]seenComponent{}
		require.NoError(t, checkComponentConsistency("chat", svc("m-a", nil), seen))

		assert.NoError(t, checkComponentConsistency("summarize", svc("", map[string]any{"model": "m-a"}), seen))
	})
}
