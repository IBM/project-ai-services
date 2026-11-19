package modelservice

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRequest() apimodels.CreateModelRequest {
	return apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeLocal,
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
		{"valid local", func(*apimodels.CreateModelRequest) {}, ""},
		{
			"valid remote",
			func(r *apimodels.CreateModelRequest) {
				r.DeploymentType, r.ProviderID = constants.DeploymentTypeRemote, "hosted_vllm"
			},
			"",
		},
		{
			"valid remote-only model",
			func(r *apimodels.CreateModelRequest) {
				r.DeploymentType, r.ProviderID, r.Model = constants.DeploymentTypeRemote, "watsonx", "ibm/granite-4-h-small"
			},
			"",
		},
		{"name bad chars", func(r *apimodels.CreateModelRequest) { r.Name = "a!b" }, "Model name may only contain"},
		{"invalid component_type", func(r *apimodels.CreateModelRequest) { r.ComponentType = "database" }, "invalid component_type"},
		{
			"unknown provider",
			func(r *apimodels.CreateModelRequest) { r.ProviderID = "nope" },
			`is not available for local provider "nope"; it is offered by: vllm-spyre (local), vllm-cpu (local), hosted_vllm (remote)`,
		},
		{
			"remote provider asked as local",
			func(r *apimodels.CreateModelRequest) { r.ProviderID = "watsonx" },
			`is not available for local provider "watsonx"`,
		},
		{"unknown model", func(r *apimodels.CreateModelRequest) { r.Model = "no/such-model" }, `invalid model "no/such-model"`},
		{
			"model of another component type",
			func(r *apimodels.CreateModelRequest) { r.ComponentType = "embedding" },
			`cannot be used as component_type "embedding"; it is a model of component_type: llm`,
		},
		{
			"model the local provider does not offer",
			func(r *apimodels.CreateModelRequest) {
				r.ProviderID, r.Model = "vllm-spyre", "ibm-granite/granite-4.1-8b"
			},
			`is not available for local provider "vllm-spyre"`,
		},
		{
			"local model asked as remote on a provider that does not list it",
			func(r *apimodels.CreateModelRequest) {
				r.DeploymentType, r.ProviderID = constants.DeploymentTypeRemote, "watsonx"
			},
			`is not available for remote provider "watsonx"`,
		},
	}

	// The deploy options are read for the runtime of the control plane.
	setControlPlaneRuntime(t, runtimeTypes.RuntimeTypePodman)

	svc := newListValidationService(t, &mockModelRepo{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)

			err := svc.validateCreateRequest(context.Background(), req)
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

// mockModelRepo records the list filters it is queried with; it returns no models. GetByID serves
// the rows it holds and records the component types it is asked for.
type mockModelRepo struct {
	dbrepo.ModelRepository
	listFilters *dbrepo.ModelFilters
	rows        []dbrepo.ModelRow
	err         error
	getID       uuid.UUID
	getTypes    []string
}

func (m *mockModelRepo) GetByID(_ context.Context, id uuid.UUID, componentTypes []string) (*dbrepo.ModelRow, error) {
	m.getID, m.getTypes = id, componentTypes

	if m.err != nil {
		return nil, m.err
	}

	for i := range m.rows {
		if m.rows[i].ID == id {
			return &m.rows[i], nil
		}
	}

	return nil, nil
}

func (m *mockModelRepo) List(_ context.Context, f *dbrepo.ModelFilters, _ []string) ([]dbrepo.ModelRow, error) {
	m.listFilters = f

	return nil, nil
}

func (m *mockModelRepo) GetCount(context.Context, *dbrepo.ModelFilters, []string) (int, error) {
	return 0, nil
}

// newListValidationService builds a ModelService whose only live part is the list path. With no
// models returned, no service dependency is ever read, so none is given.
func newListValidationService(t *testing.T, repo *mockModelRepo) *ModelService {
	t.Helper()

	return NewModelService(nil, nil, repo, nil, nil, setupTestCatalogProvider(t), "")
}

func listRequest() apimodels.ListModelsRequest {
	return apimodels.ListModelsRequest{Page: 1, PageSize: 20, CreatedBy: "alice"}
}

func TestListModels_RejectsUnknownFilters(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*apimodels.ListModelsRequest)
		wantMsg string
	}{
		{"unknown deployment type", func(r *apimodels.ListModelsRequest) { r.DeploymentType = "cloud" }, "deployment_type must be 'local' or 'remote'"},
		{"deployment type is case sensitive", func(r *apimodels.ListModelsRequest) { r.DeploymentType = "Local" }, "deployment_type must be"},
		{"unknown provider", func(r *apimodels.ListModelsRequest) { r.ProviderIDs = []string{"nope"} }, `invalid provider_id "nope"`},
		{
			"one unknown provider among known ones",
			func(r *apimodels.ListModelsRequest) { r.ProviderIDs = []string{"vllm-spyre", "nope"} },
			`invalid provider_id "nope"`,
		},
		{
			"one unknown model among known ones",
			func(r *apimodels.ListModelsRequest) { r.Models = []string{"ibm/granite-4-h-small", "no/such-model"} },
			`invalid model "no/such-model"`,
		},
		{
			"remote provider asked as local",
			func(r *apimodels.ListModelsRequest) {
				r.ProviderIDs, r.DeploymentType = []string{"watsonx"}, constants.DeploymentTypeLocal
			},
			`invalid provider_id "watsonx"`,
		},
		{"unknown model", func(r *apimodels.ListModelsRequest) { r.Models = []string{"no/such-model"} }, `invalid model "no/such-model"`},
		{"unknown status", func(r *apimodels.ListModelsRequest) { r.Status = "Sleeping" }, `invalid status "Sleeping"`},
		{
			"remote status asked as local",
			func(r *apimodels.ListModelsRequest) {
				r.Status, r.DeploymentType = "connected", constants.DeploymentTypeLocal
			},
			`invalid status "connected"`,
		},
		{
			"local status asked as remote",
			func(r *apimodels.ListModelsRequest) {
				r.Status, r.DeploymentType = "Running", constants.DeploymentTypeRemote
			},
			`invalid status "Running"`,
		},
		{"status is case sensitive", func(r *apimodels.ListModelsRequest) { r.Status = "running" }, `invalid status "running"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockModelRepo{}
			svc := newListValidationService(t, repo)

			req := listRequest()
			tt.mutate(&req)

			_, err := svc.ListModels(context.Background(), req)

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, http.StatusBadRequest, valErr.Code)
			assert.Contains(t, valErr.Message, tt.wantMsg)
			assert.Nil(t, repo.listFilters, "the repository must not be queried with a filter that cannot match")
		})
	}
}

func TestListModels_AcceptsKnownFilters(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apimodels.ListModelsRequest)
	}{
		{"local deployment type", func(r *apimodels.ListModelsRequest) { r.DeploymentType = constants.DeploymentTypeLocal }},
		{"remote deployment type", func(r *apimodels.ListModelsRequest) { r.DeploymentType = constants.DeploymentTypeRemote }},
		{"local provider", func(r *apimodels.ListModelsRequest) { r.ProviderIDs = []string{"vllm-spyre"} }},
		{"remote provider", func(r *apimodels.ListModelsRequest) { r.ProviderIDs = []string{"watsonx"} }},
		{"local and remote providers together", func(r *apimodels.ListModelsRequest) {
			r.ProviderIDs = []string{"vllm-spyre", "watsonx"}
		}},
		{"several models", func(r *apimodels.ListModelsRequest) {
			r.Models = []string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"}
		}},
		{"provider with matching type", func(r *apimodels.ListModelsRequest) {
			r.ProviderIDs, r.DeploymentType = []string{"hosted_vllm"}, constants.DeploymentTypeRemote
		}},
		{"remote-only model", func(r *apimodels.ListModelsRequest) { r.Models = []string{"ibm/granite-4-h-small"} }},
		{"local status", func(r *apimodels.ListModelsRequest) { r.Status = "Downloading" }},
		{"remote status", func(r *apimodels.ListModelsRequest) { r.Status = "offline" }},
		{"status with matching local type", func(r *apimodels.ListModelsRequest) {
			r.Status, r.DeploymentType = "Running", constants.DeploymentTypeLocal
		}},
		{"status with matching remote type", func(r *apimodels.ListModelsRequest) {
			r.Status, r.DeploymentType = "connected", constants.DeploymentTypeRemote
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newListValidationService(t, &mockModelRepo{})

			req := listRequest()
			tt.mutate(&req)

			_, err := svc.ListModels(context.Background(), req)
			require.NoError(t, err)
		})
	}
}

func TestCheckDeployOptions(t *testing.T) {
	options := func(componentType string, providers ...types.ModelDeployProvider) *types.ModelDeployOptions {
		return &types.ModelDeployOptions{
			ID:            "ibm-granite/granite-3.3-8b-instruct",
			ComponentType: types.ComponentType{ID: componentType},
			Providers:     providers,
		}
	}
	local := func(id string) types.ModelDeployProvider {
		return types.ModelDeployProvider{ID: id, DeploymentType: constants.DeploymentTypeLocal}
	}
	remote := func(id string) types.ModelDeployProvider {
		return types.ModelDeployProvider{ID: id, DeploymentType: constants.DeploymentTypeRemote}
	}

	tests := []struct {
		name    string
		mutate  func(*apimodels.CreateModelRequest)
		options *types.ModelDeployOptions
		wantMsg string
	}{
		{
			name:    "local provider offers the model",
			mutate:  func(*apimodels.CreateModelRequest) {},
			options: options("llm", local("vllm-spyre"), local("vllm-cpu"), remote("hosted_vllm")),
		},
		{
			name: "remote provider offers the model",
			mutate: func(r *apimodels.CreateModelRequest) {
				r.DeploymentType, r.ProviderID = constants.DeploymentTypeRemote, "hosted_vllm"
			},
			options: options("llm", local("vllm-cpu"), remote("hosted_vllm")),
		},
		{
			name:    "model of another component type",
			mutate:  func(r *apimodels.CreateModelRequest) { r.ComponentType = "embedding" },
			options: options("llm", local("vllm-cpu")),
			wantMsg: `cannot be used as component_type "embedding"; it is a model of component_type: llm`,
		},
		{
			name: "provider id exists but for the other deployment type",
			mutate: func(r *apimodels.CreateModelRequest) {
				r.DeploymentType = constants.DeploymentTypeRemote
			},
			options: options("llm", local("vllm-cpu"), remote("hosted_vllm")),
			wantMsg: `is not available for remote provider "vllm-cpu"; it is offered by: vllm-cpu (local), hosted_vllm (remote)`,
		},
		{
			name:    "unknown provider",
			mutate:  func(r *apimodels.CreateModelRequest) { r.ProviderID = "nope" },
			options: options("llm", local("vllm-cpu")),
			wantMsg: `is not available for local provider "nope"; it is offered by: vllm-cpu (local)`,
		},
		{
			name:    "model with no provider",
			mutate:  func(*apimodels.CreateModelRequest) {},
			options: options("llm"),
			wantMsg: `is not available for any provider`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)

			err := checkDeployOptions(req, tt.options)
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
