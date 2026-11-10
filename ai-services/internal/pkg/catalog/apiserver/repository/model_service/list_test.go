package modelservice

import (
	"context"
	"errors"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockModelRepo struct {
	dbrepo.ModelRepository
	rows  []dbrepo.ModelRow
	total int
	err   error

	listFilters  *dbrepo.ModelFilters
	countFilters *dbrepo.ModelFilters
	// listTypes, countTypes and getTypes are the component types passed to each call.
	listTypes, countTypes, getTypes []string
	// getID records the last GetByID call.
	getID uuid.UUID
}

func (m *mockModelRepo) List(_ context.Context, f *dbrepo.ModelFilters, componentTypes []string) ([]dbrepo.ModelRow, error) {
	m.listFilters, m.listTypes = f, componentTypes

	return m.rows, m.err
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

func (m *mockModelRepo) GetCount(_ context.Context, f *dbrepo.ModelFilters, componentTypes []string) (int, error) {
	m.countFilters, m.countTypes = f, componentTypes

	return m.total, m.err
}

type mockSvcDepRepo struct {
	dbrepo.ServiceDependencyRepository
	counts map[dbmodels.DependencyType]map[uuid.UUID]int
	err    error
	// linked is what GetLinkedServiceEndpoints returns per dependency type.
	linked map[dbmodels.DependencyType][]dbrepo.LinkedServiceRow
	// asked records the ids requested per dependency type.
	asked map[dbmodels.DependencyType][]uuid.UUID
	// linkedCalls counts GetLinkedServiceEndpoints calls.
	linkedCalls int
}

func (m *mockSvcDepRepo) GetServiceCountByDependency(_ context.Context, ids []uuid.UUID, t dbmodels.DependencyType) (map[uuid.UUID]int, error) {
	if m.asked == nil {
		m.asked = map[dbmodels.DependencyType][]uuid.UUID{}
	}
	m.asked[t] = ids

	return m.counts[t], m.err
}

func (m *mockSvcDepRepo) GetLinkedServiceEndpoints(_ context.Context, _ uuid.UUID, t dbmodels.DependencyType) ([]dbrepo.LinkedServiceRow, error) {
	m.linkedCalls++

	return m.linked[t], m.err
}

func newListService(t *testing.T, repo *mockModelRepo, deps *mockSvcDepRepo) *ModelService {
	t.Helper()

	return NewModelService(nil, nil, repo, deps, nil, setupTestCatalogProvider(t), "")
}

func listRequest() apimodels.ListModelsRequest {
	return apimodels.ListModelsRequest{Page: 1, PageSize: 20, CreatedBy: "alice"}
}

func TestListModels_PassesFiltersAndPaging(t *testing.T) {
	repo := &mockModelRepo{}
	svc := newListService(t, repo, &mockSvcDepRepo{})

	_, err := svc.ListModels(context.Background(), apimodels.ListModelsRequest{
		Page: 3, PageSize: 10, CreatedBy: "alice", ComponentType: "llm",
		DeploymentType: constants.DeploymentTypeLocal, ProviderIDs: []string{"vllm-spyre", "vllm-cpu"},
		Models: []string{"ibm-granite/granite-3.3-8b-instruct", "ibm/granite-4-h-small"}, Status: "Running",
	})
	require.NoError(t, err)

	want := &dbrepo.ModelFilters{
		CreatedBy: "alice", DeploymentType: "local", ComponentType: "llm", Providers: []string{"vllm-spyre", "vllm-cpu"},
		Models: []string{"ibm-granite/granite-3.3-8b-instruct", "ibm/granite-4-h-small"}, Status: "Running", Limit: 10, Offset: 20,
	}
	assert.Equal(t, want, repo.listFilters)
	assert.Equal(t, want, repo.countFilters, "count and list must filter identically")
}

// Only the component types the catalog declares as models count as models.
func TestListModels_RestrictsToCatalogModelTypes(t *testing.T) {
	repo := &mockModelRepo{}
	svc := newListService(t, repo, &mockSvcDepRepo{})

	_, err := svc.ListModels(context.Background(), listRequest())
	require.NoError(t, err)

	assert.Contains(t, repo.listTypes, "llm")
	assert.NotContains(t, repo.listTypes, "vector_db", "non-model components are not models")
	assert.Equal(t, repo.listTypes, repo.countTypes, "count and list must use the same types")
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
		{"status is case sensitive", func(r *apimodels.ListModelsRequest) { r.Status = "running" }, `invalid status "running"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockModelRepo{}
			svc := newListService(t, repo, &mockSvcDepRepo{})

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newListService(t, &mockModelRepo{}, &mockSvcDepRepo{})

			req := listRequest()
			tt.mutate(&req)

			_, err := svc.ListModels(context.Background(), req)
			require.NoError(t, err)
		})
	}
}

func TestListModels_Pagination(t *testing.T) {
	tests := []struct {
		name                  string
		page, pageSize, total int
		wantPages             int
		wantNext, wantPrev    bool
	}{
		{"empty", 1, 20, 0, 0, false, false},
		{"single page", 1, 20, 2, 1, false, false},
		{"first of three", 1, 20, 45, 3, true, false},
		{"last of three", 3, 20, 45, 3, false, true},
		{"exact multiple", 2, 20, 40, 2, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newListService(t, &mockModelRepo{total: tt.total}, &mockSvcDepRepo{})

			req := listRequest()
			req.Page, req.PageSize = tt.page, tt.pageSize

			resp, err := svc.ListModels(context.Background(), req)
			require.NoError(t, err)

			assert.NotNil(t, resp.Data, "data must serialise as [] and not null")
			assert.Equal(t, tt.total, resp.Pagination.TotalItems)
			assert.Equal(t, tt.wantPages, resp.Pagination.TotalPages)
			assert.Equal(t, tt.wantNext, resp.Pagination.HasNext)
			assert.Equal(t, tt.wantPrev, resp.Pagination.HasPrev)
		})
	}
}

func TestListModels_LocalAndRemoteItems(t *testing.T) {
	localID, remoteID := uuid.New(), uuid.New()
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	repo := &mockModelRepo{
		total: 2,
		rows: []dbrepo.ModelRow{
			{
				ID: localID, DeploymentType: "local", Name: "granite-llm", ComponentType: "llm",
				Provider: "vllm-spyre", Model: "ibm-granite/granite-3.3-8b-instruct",
				Status: "Running", Message: "Model running", WorkerID: "w-1", WorkerName: "lpar-1", WorkerRuntimeType: "podman",
				CreatedAt: created, UpdatedAt: created,
			},
			{
				ID: remoteID, DeploymentType: "remote", Name: "prod-watsonx", ComponentType: "llm",
				Provider: "watsonx", Model: "ibm/granite-4-h-small", Status: "connected",
				Params:    map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com"},
				CreatedAt: created, UpdatedAt: created,
			},
		},
	}
	deps := &mockSvcDepRepo{counts: map[dbmodels.DependencyType]map[uuid.UUID]int{
		dbmodels.DependencyTypeComponent: {localID: 2},
		dbmodels.DependencyTypeConnector: {remoteID: 1},
	}}

	resp, err := newListService(t, repo, deps).ListModels(context.Background(), listRequest())
	require.NoError(t, err)
	require.Len(t, resp.Data, 2)

	local, remote := resp.Data[0], resp.Data[1]

	assert.Equal(t, localID.String(), local.ID)
	assert.Equal(t, constants.DeploymentTypeLocal, local.DeploymentType)
	assert.Equal(t, "vllm-spyre", local.Provider.ID)
	assert.NotEqual(t, "vllm-spyre", local.Provider.Name, "the display name comes from the catalog")
	require.NotNil(t, local.Worker)
	assert.Equal(t, types.ApplicationWorker{ID: "w-1", Name: "lpar-1", RuntimeType: "podman"}, *local.Worker)
	assert.Equal(t, 2, local.ConnectedServices, "local models are counted as component dependencies")

	assert.Equal(t, constants.DeploymentTypeRemote, remote.DeploymentType)
	assert.Nil(t, remote.Worker)
	assert.Equal(t, "connected", remote.Status)
	assert.Equal(t, "https://us-south.ml.cloud.ibm.com", remote.Params["api_base"])
	assert.Equal(t, 1, remote.ConnectedServices, "remote models are counted as connector dependencies")

	assert.Equal(t, []uuid.UUID{localID}, deps.asked[dbmodels.DependencyTypeComponent])
	assert.Equal(t, []uuid.UUID{remoteID}, deps.asked[dbmodels.DependencyTypeConnector])
}

// A model must still be listed when the catalog no longer knows its provider
// (e.g. a removed bundle), instead of failing the whole page.
func TestListModels_FallsBackToProviderIDWhenCatalogHasNoEntry(t *testing.T) {
	repo := &mockModelRepo{
		total: 1,
		rows: []dbrepo.ModelRow{{
			ID: uuid.New(), DeploymentType: "local", Name: "old-model", ComponentType: "llm",
			Provider: "retired-provider", Model: "ibm-granite/granite-3.3-8b-instruct", Status: "Running",
		}},
	}

	resp, err := newListService(t, repo, &mockSvcDepRepo{}).ListModels(context.Background(), listRequest())
	require.NoError(t, err)
	require.Len(t, resp.Data, 1)

	item := resp.Data[0]
	assert.Equal(t, types.ProviderInfo{ID: "retired-provider", Name: "retired-provider"}, item.Provider)
	assert.Equal(t, 0, item.ConnectedServices)
}

func TestListModels_RepositoryErrors(t *testing.T) {
	boom := errors.New("db down")

	t.Run("models", func(t *testing.T) {
		_, err := newListService(t, &mockModelRepo{err: boom}, &mockSvcDepRepo{}).ListModels(context.Background(), listRequest())
		require.ErrorIs(t, err, boom)
	})

	t.Run("service counts", func(t *testing.T) {
		repo := &mockModelRepo{total: 1, rows: []dbrepo.ModelRow{{ID: uuid.New(), DeploymentType: "local"}}}

		_, err := newListService(t, repo, &mockSvcDepRepo{err: boom}).ListModels(context.Background(), listRequest())
		require.ErrorIs(t, err, boom)
	})
}
