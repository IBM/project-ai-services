package modelservice

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGetService(t *testing.T, repo *mockModelRepo, comps *mockComponentRepo, deps *mockSvcDepRepo) *ModelService {
	t.Helper()

	return NewModelService(comps, nil, repo, deps, nil, setupTestCatalogProvider(t), "")
}

const testGatewayEndpoint = "https://litellm.example.com"

// newGetServiceWithGateway is newGetService with a configured gateway public endpoint.
func newGetServiceWithGateway(t *testing.T, repo *mockModelRepo, deps *mockSvcDepRepo, endpoint string) *ModelService {
	t.Helper()

	svc := newGetService(t, repo, nil, deps)
	svc.gatewayPublicEndpoint = endpoint

	return svc
}

func TestGetModelByID_Local(t *testing.T) {
	id, workerID := uuid.New(), uuid.New()
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	repo := &mockModelRepo{rows: []dbrepo.ModelRow{{
		ID: id, DeploymentType: "local", Name: "granite-llm", ComponentType: "llm",
		Provider: "vllm-spyre", Model: "ibm-granite/granite-3.3-8b-instruct",
		Params: map[string]any{"max_len": 4096}, Status: "Running", Message: "Model running",
		WorkerID: workerID.String(), WorkerName: "lpar-1", WorkerRuntimeType: "podman",
		CreatedBy: "alice", CreatedAt: created, UpdatedAt: created,
	}}}

	deps := &mockSvcDepRepo{linked: map[dbmodels.DependencyType][]dbrepo.LinkedServiceRow{
		dbmodels.DependencyTypeComponent: {
			{ApplicationID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), ApplicationName: "zeta-app"},
			{ApplicationID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), ApplicationName: "alpha-app"},
			// A second service of the same application must not list it twice.
			{ApplicationID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), ApplicationName: "alpha-app"},
		},
	}, counts: map[dbmodels.DependencyType]map[uuid.UUID]int{
		// Three services across two applications: the count is of services, not applications.
		dbmodels.DependencyTypeComponent: {id: 3},
	}}

	got, err := newGetServiceWithGateway(t, repo, deps, testGatewayEndpoint).GetModelByID(context.Background(), id, "alice")
	require.NoError(t, err)

	assert.Equal(t, id, repo.getID)
	assert.Contains(t, repo.getTypes, "llm", "the lookup is limited to the catalog's model types")
	assert.NotContains(t, repo.getTypes, "vector_db")

	assert.Equal(t, id.String(), got.ID)
	assert.Equal(t, constants.DeploymentTypeLocal, got.DeploymentType)
	assert.Equal(t, "granite-llm", got.Name)
	assert.Equal(t, "vllm-spyre", got.Provider.ID)
	assert.NotEqual(t, "vllm-spyre", got.Provider.Name, "the display name comes from the catalog")
	assert.Equal(t, &types.ApplicationWorker{ID: workerID.String(), Name: "lpar-1", RuntimeType: "podman"}, got.Worker)
	assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", got.Model)
	assert.Equal(t, map[string]any{"max_len": 4096}, got.Params)
	assert.Equal(t, "Running", got.Status)
	assert.Equal(t, "Model running", got.Message)
	assert.Equal(t, []apimodels.ModelEndpoint{{Type: "external", URL: testGatewayEndpoint}}, got.Endpoints)
	assert.Equal(t, []apimodels.ModelApplication{
		{ID: "11111111-1111-1111-1111-111111111111", Name: "alpha-app"},
		{ID: "22222222-2222-2222-2222-222222222222", Name: "zeta-app"},
	}, got.Applications, "one entry per application, sorted by name")
	assert.Equal(t, 3, got.ConnectedServices)
	assert.Equal(t, created, got.CreatedAt)
}

// The endpoint is always reported, local or remote, even when no gateway endpoint is configured.
func TestGetModelByID_EndpointNotConfigured(t *testing.T) {
	for _, deploymentType := range []constants.DeploymentType{constants.DeploymentTypeLocal, constants.DeploymentTypeRemote} {
		t.Run(string(deploymentType), func(t *testing.T) {
			id := uuid.New()
			repo := &mockModelRepo{rows: []dbrepo.ModelRow{{ID: id, DeploymentType: deploymentType, Provider: "watsonx", CreatedBy: "alice"}}}

			got, err := newGetService(t, repo, nil, &mockSvcDepRepo{}).GetModelByID(context.Background(), id, "alice")
			require.NoError(t, err)

			assert.Equal(t, []apimodels.ModelEndpoint{{Type: "external", URL: ""}}, got.Endpoints)
		})
	}
}

func TestGetModelByID_Remote(t *testing.T) {
	id := uuid.New()

	repo := &mockModelRepo{rows: []dbrepo.ModelRow{{
		ID: id, DeploymentType: "remote", Name: "prod-watsonx", ComponentType: "llm",
		Provider: "watsonx", Model: "ibm/granite-4-h-small", Status: "connected",
		Params:    map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com"},
		CreatedBy: "alice",
	}}}

	// A remote model has no component row: any component lookup would fail the test.
	deps := &mockSvcDepRepo{linked: map[dbmodels.DependencyType][]dbrepo.LinkedServiceRow{
		dbmodels.DependencyTypeConnector: {{ApplicationID: uuid.New(), ApplicationName: "my-rag-app"}},
		// Must be ignored: a remote model is linked as a connector, never as a component.
		dbmodels.DependencyTypeComponent: {{ApplicationID: uuid.New(), ApplicationName: "unrelated"}},
	}, counts: map[dbmodels.DependencyType]map[uuid.UUID]int{
		dbmodels.DependencyTypeConnector: {id: 1},
		dbmodels.DependencyTypeComponent: {id: 9},
	}}

	got, err := newGetServiceWithGateway(t, repo, deps, testGatewayEndpoint).GetModelByID(context.Background(), id, "alice")
	require.NoError(t, err)

	assert.Equal(t, constants.DeploymentTypeRemote, got.DeploymentType)
	assert.Equal(t, "Watsonx", got.Provider.Name)
	assert.Nil(t, got.Worker)
	assert.Equal(t, []apimodels.ModelEndpoint{{Type: "external", URL: testGatewayEndpoint}}, got.Endpoints,
		"a remote model is called through the same LiteLLM endpoint")
	assert.Equal(t, "https://us-south.ml.cloud.ibm.com", got.Params["api_base"])
	require.Len(t, got.Applications, 1)
	assert.Equal(t, "my-rag-app", got.Applications[0].Name)
	assert.Equal(t, 1, got.ConnectedServices, "counted as a connector, never as a component")
}

func TestGetModelByID_NoApplicationsIsAnEmptyList(t *testing.T) {
	id := uuid.New()
	repo := &mockModelRepo{rows: []dbrepo.ModelRow{{ID: id, DeploymentType: "remote", Provider: "watsonx", CreatedBy: "alice"}}}

	got, err := newGetService(t, repo, nil, &mockSvcDepRepo{}).GetModelByID(context.Background(), id, "alice")
	require.NoError(t, err)

	assert.NotNil(t, got.Applications, "applications must serialise as [] and not null")
	assert.Empty(t, got.Applications)
	assert.Zero(t, got.ConnectedServices)
}

func TestGetModelByID_FallsBackToProviderIDWhenCatalogHasNoEntry(t *testing.T) {
	id := uuid.New()
	repo := &mockModelRepo{rows: []dbrepo.ModelRow{{ID: id, DeploymentType: "remote", Provider: "retired-provider", CreatedBy: "alice"}}}

	got, err := newGetService(t, repo, nil, &mockSvcDepRepo{}).GetModelByID(context.Background(), id, "alice")
	require.NoError(t, err)

	assert.Equal(t, types.ProviderInfo{ID: "retired-provider", Name: "retired-provider"}, got.Provider)
}

// A missing model is a 404; a model created by someone else is a 403, as for applications. In both
// cases nothing about the model may be read.
func TestGetModelByID_NotFoundAndForbidden(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name     string
		rows     []dbrepo.ModelRow
		wantCode int
	}{
		{"missing", nil, http.StatusNotFound},
		{"owned by someone else", []dbrepo.ModelRow{{ID: id, DeploymentType: "remote", Provider: "watsonx", CreatedBy: "bob"}}, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := &mockSvcDepRepo{}
			got, err := newGetService(t, &mockModelRepo{rows: tt.rows}, nil, deps).GetModelByID(context.Background(), id, "alice")

			assert.Nil(t, got)

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, tt.wantCode, valErr.Code)
			assert.Zero(t, deps.linkedCalls, "nothing about the model may be read")
			assert.Empty(t, deps.asked, "nothing about the model may be counted")
		})
	}
}

func TestGetModelByID_RepositoryErrors(t *testing.T) {
	id := uuid.New()
	row := dbrepo.ModelRow{ID: id, DeploymentType: "local", Provider: "vllm-spyre", CreatedBy: "alice"}
	boom := errors.New("boom")

	tests := []struct {
		name string
		repo *mockModelRepo
		deps *mockSvcDepRepo
	}{
		{"model lookup", &mockModelRepo{err: boom}, &mockSvcDepRepo{}},
		{"linked applications", &mockModelRepo{rows: []dbrepo.ModelRow{row}}, &mockSvcDepRepo{err: boom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newGetService(t, tt.repo, newMockComponentRepo(), tt.deps).GetModelByID(context.Background(), id, "alice")

			require.ErrorIs(t, err, boom)

			var valErr *validators.ValidationError
			assert.False(t, errors.As(err, &valErr), "an internal failure must not look like a client error")
		})
	}
}
