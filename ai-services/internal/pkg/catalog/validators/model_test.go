package validators

import (
	"net/http"
	"testing"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateModelComponentFields(t *testing.T) {
	tests := []struct {
		name    string
		comp    apimodels.Component
		wantErr string
	}{
		{"new deployment", apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1", Model: "m"}, ""},
		{"instance only", apimodels.Component{ComponentType: "llm", InstanceID: "x"}, ""},
		{"instance with provider", apimodels.Component{ComponentType: "llm", InstanceID: "x", ProviderID: "vllm-cpu"}, "must be omitted"},
		{"instance with params", apimodels.Component{ComponentType: "llm", InstanceID: "x", Params: map[string]any{"a": 1}}, "must be omitted"},
		{"missing provider", apimodels.Component{ComponentType: "llm", Version: "1", Model: "m"}, "requires provider_id and version"},
		{"missing model", apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1"}, "requires a model"},
		{"model in params", apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1", Model: "m", Params: map[string]any{"model": "m"}}, "top-level field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFieldsResult(t, validateModelComponentFields(tt.comp), tt.wantErr)
		})
	}
}

func TestValidateNonModelComponentFields(t *testing.T) {
	tests := []struct {
		name    string
		comp    apimodels.Component
		wantErr string
	}{
		{"valid", apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch", Version: "1"}, ""},
		{"missing version", apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch"}, "requires provider_id and version"},
		{"model set", apimodels.Component{ComponentType: "vector_store", ProviderID: "o", Version: "1", Model: "m"}, "not a model component"},
		{"instance set", apimodels.Component{ComponentType: "vector_store", ProviderID: "o", Version: "1", InstanceID: "x"}, "not a model component"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFieldsResult(t, validateNonModelComponentFields(tt.comp), tt.wantErr)
		})
	}
}

func assertFieldsResult(t *testing.T, err error, wantErr string) {
	t.Helper()

	if wantErr == "" {
		require.NoError(t, err)

		return
	}

	var verr *ValidationError

	require.ErrorAs(t, err, &verr)
	assert.Equal(t, http.StatusBadRequest, verr.Code)
	assert.Contains(t, verr.Message, wantErr)
}

func TestCheckModelDeployOptions(t *testing.T) {
	options := &types.ModelDeployOptions{
		ComponentType: types.ComponentType{ID: "llm"},
		Providers: []types.ModelDeployProvider{
			{ID: "vllm-cpu", DeploymentType: constants.DeploymentTypeLocal},
		},
	}

	require.NoError(t, CheckModelDeployOptions(options, "m", "llm", "vllm-cpu", constants.DeploymentTypeLocal))
	assertFieldsResult(t, CheckModelDeployOptions(options, "m", "embedding", "vllm-cpu", constants.DeploymentTypeLocal), "cannot be used as component_type")
	assertFieldsResult(t, CheckModelDeployOptions(options, "m", "llm", "vllm-spyre", constants.DeploymentTypeLocal), "is not available for local provider")
}

func TestCheckComponentConsistencyModels(t *testing.T) {
	llm := func(model string, params map[string]any) apimodels.Component {
		return apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1", Model: model, Params: params}
	}
	service := func(c apimodels.Component) apimodels.Service {
		return apimodels.Service{Components: []apimodels.Component{c}}
	}

	check := func(first, second apimodels.Component) error {
		seen := map[string]seenComponent{}
		if err := checkComponentConsistency("chat", service(first), seen); err != nil {
			return err
		}

		return checkComponentConsistency("digitize", service(second), seen)
	}

	t.Run("the same model and params are one deployment", func(t *testing.T) {
		require.NoError(t, check(
			llm("ibm/granite", map[string]any{"a": 1, "b": 2}),
			llm("ibm/granite", map[string]any{"b": 2, "a": 1}),
		))
	})

	t.Run("a different model is a mismatch", func(t *testing.T) {
		err := check(llm("ibm/granite", nil), llm("ibm/other", nil))

		assertFieldsResult(t, err, "Different values given for 'model'")
	})

	t.Run("different params are a mismatch", func(t *testing.T) {
		err := check(llm("ibm/granite", map[string]any{"a": 1}), llm("ibm/granite", map[string]any{"a": 2}))

		assertFieldsResult(t, err, "Different values given for 'a'")
	})

	t.Run("an existing instance is not compared", func(t *testing.T) {
		require.NoError(t, check(
			llm("ibm/granite", nil),
			apimodels.Component{ComponentType: "llm", InstanceID: "5f0c0b1e-0000-0000-0000-000000000000"},
		))
	})
}
