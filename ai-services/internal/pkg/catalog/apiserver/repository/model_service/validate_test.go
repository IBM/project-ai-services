package modelservice

import (
	"net/http"
	"testing"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRequest() apimodels.CreateModelRequest {
	return apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeLocal,
		ComponentType:  "llm",
		Name:           "granite-llm",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}
}

func TestValidateCreateRequest(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*apimodels.CreateModelRequest)
		wantMsg string
	}{
		{"valid", func(*apimodels.CreateModelRequest) {}, ""},
		{"name bad chars", func(r *apimodels.CreateModelRequest) { r.Name = "a!b" }, "name must contain only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)

			err := validateCreateRequest(req)
			if tt.wantMsg == "" {
				require.NoError(t, err)

				return
			}

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, http.StatusBadRequest, valErr.Code)
			assert.Contains(t, valErr.Message, tt.wantMsg)
		})
	}
}
