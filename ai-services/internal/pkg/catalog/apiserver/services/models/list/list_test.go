package list

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockModelRepo serves the rows and total it holds and records the filters and component types
// of each call.
type mockModelRepo struct {
	dbrepo.ModelRepository
	rows  []dbrepo.ModelRow
	total int
	// listErr and countErr fail List and GetCount.
	listErr, countErr error

	listFilters  *dbrepo.ModelFilters
	countFilters *dbrepo.ModelFilters
	// listTypes and countTypes are the component types passed to each call.
	listTypes, countTypes []string
}

func (m *mockModelRepo) List(_ context.Context, f *dbrepo.ModelFilters, componentTypes []string) ([]dbrepo.ModelRow, error) {
	m.listFilters, m.listTypes = f, componentTypes

	return m.rows, m.listErr
}

func (m *mockModelRepo) GetCount(_ context.Context, f *dbrepo.ModelFilters, componentTypes []string) (int, error) {
	m.countFilters, m.countTypes = f, componentTypes

	return m.total, m.countErr
}

type mockSvcDepRepo struct {
	dbrepo.ServiceDependencyRepository
	counts map[dbmodels.DependencyType]map[uuid.UUID]int
	err    error
	// asked records the ids requested per dependency type.
	asked map[dbmodels.DependencyType][]uuid.UUID
}

func (m *mockSvcDepRepo) GetServiceCountByDependency(_ context.Context, ids []uuid.UUID, t dbmodels.DependencyType) (map[uuid.UUID]int, error) {
	if m.asked == nil {
		m.asked = map[dbmodels.DependencyType][]uuid.UUID{}
	}
	m.asked[t] = ids

	return m.counts[t], m.err
}

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

func newLister(t *testing.T, repo *mockModelRepo, deps *mockSvcDepRepo) *Lister {
	t.Helper()

	return New(repo, deps, setupTestCatalogProvider(t))
}

func listRequest() apimodels.ListModelsRequest {
	return apimodels.ListModelsRequest{Page: 1, PageSize: 20, CreatedBy: "alice"}
}

func TestList_PassesFiltersAndPaging(t *testing.T) {
	repo := &mockModelRepo{}
	svc := newLister(t, repo, &mockSvcDepRepo{})

	_, err := svc.List(context.Background(), apimodels.ListModelsRequest{
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
func TestList_RestrictsToCatalogModelTypes(t *testing.T) {
	repo := &mockModelRepo{}
	svc := newLister(t, repo, &mockSvcDepRepo{})

	_, err := svc.List(context.Background(), listRequest())
	require.NoError(t, err)

	assert.Contains(t, repo.listTypes, "llm")
	assert.NotContains(t, repo.listTypes, "vector_db", "non-model components are not models")
	assert.Equal(t, repo.listTypes, repo.countTypes, "count and list must use the same types")
}

func TestList_Pagination(t *testing.T) {
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
			svc := newLister(t, &mockModelRepo{total: tt.total}, &mockSvcDepRepo{})

			req := listRequest()
			req.Page, req.PageSize = tt.page, tt.pageSize

			resp, err := svc.List(context.Background(), req)
			require.NoError(t, err)

			assert.NotNil(t, resp.Data, "data must serialise as [] and not null")
			assert.Equal(t, tt.total, resp.Pagination.TotalItems)
			assert.Equal(t, tt.wantPages, resp.Pagination.TotalPages)
			assert.Equal(t, tt.wantNext, resp.Pagination.HasNext)
			assert.Equal(t, tt.wantPrev, resp.Pagination.HasPrev)
		})
	}
}

func TestList_LocalAndRemoteItems(t *testing.T) {
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

	resp, err := newLister(t, repo, deps).List(context.Background(), listRequest())
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

// A dependency type with no model on the page is not queried.
func TestList_SkipsDependencyTypesWithoutModels(t *testing.T) {
	localID, remoteID := uuid.New(), uuid.New()

	tests := []struct {
		name         string
		rows         []dbrepo.ModelRow
		wantAsked    []dbmodels.DependencyType
		wantNotAsked dbmodels.DependencyType
	}{
		{
			name:         "only local models",
			rows:         []dbrepo.ModelRow{{ID: localID, DeploymentType: constants.DeploymentTypeLocal}},
			wantAsked:    []dbmodels.DependencyType{dbmodels.DependencyTypeComponent},
			wantNotAsked: dbmodels.DependencyTypeConnector,
		},
		{
			name:         "only remote models",
			rows:         []dbrepo.ModelRow{{ID: remoteID, DeploymentType: constants.DeploymentTypeRemote}},
			wantAsked:    []dbmodels.DependencyType{dbmodels.DependencyTypeConnector},
			wantNotAsked: dbmodels.DependencyTypeComponent,
		},
		{
			name:      "no models",
			rows:      nil,
			wantAsked: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := &mockSvcDepRepo{}

			_, err := newLister(t, &mockModelRepo{rows: tt.rows}, deps).List(context.Background(), listRequest())
			require.NoError(t, err)

			assert.Len(t, deps.asked, len(tt.wantAsked))

			for _, depType := range tt.wantAsked {
				assert.Contains(t, deps.asked, depType)
			}

			if tt.wantNotAsked != "" {
				assert.NotContains(t, deps.asked, tt.wantNotAsked)
			}
		})
	}
}

// A model must still be listed when the catalog no longer knows its provider
// (e.g. a removed bundle), instead of failing the whole page.
func TestList_FallsBackToProviderIDWhenCatalogHasNoEntry(t *testing.T) {
	repo := &mockModelRepo{
		total: 1,
		rows: []dbrepo.ModelRow{{
			ID: uuid.New(), DeploymentType: "local", Name: "old-model", ComponentType: "llm",
			Provider: "retired-provider", Model: "ibm-granite/granite-3.3-8b-instruct", Status: "Running",
		}},
	}

	resp, err := newLister(t, repo, &mockSvcDepRepo{}).List(context.Background(), listRequest())
	require.NoError(t, err)
	require.Len(t, resp.Data, 1)

	item := resp.Data[0]
	assert.Equal(t, types.ProviderInfo{ID: "retired-provider", Name: "retired-provider"}, item.Provider)
	assert.Equal(t, 0, item.ConnectedServices)
}

func TestList_RepositoryErrors(t *testing.T) {
	boom := errors.New("db down")
	rows := []dbrepo.ModelRow{{ID: uuid.New(), DeploymentType: "local"}}

	tests := []struct {
		name string
		repo *mockModelRepo
		deps *mockSvcDepRepo
	}{
		{"count", &mockModelRepo{countErr: boom}, &mockSvcDepRepo{}},
		{"list", &mockModelRepo{total: 1, rows: rows, listErr: boom}, &mockSvcDepRepo{}},
		{"service counts", &mockModelRepo{total: 1, rows: rows}, &mockSvcDepRepo{err: boom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newLister(t, tt.repo, tt.deps).List(context.Background(), listRequest())

			require.ErrorIs(t, err, boom)
		})
	}
}
