package get

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
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSvcDepRepo struct {
	dbrepo.ServiceDependencyRepository
	counts map[dbmodels.DependencyType]map[uuid.UUID]int
	// countErr and linkedErr fail GetServiceCountByDependency and GetLinkedServiceEndpoints.
	countErr, linkedErr error
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

	return m.counts[t], m.countErr
}

func (m *mockSvcDepRepo) GetLinkedServiceEndpoints(_ context.Context, _ uuid.UUID, t dbmodels.DependencyType) ([]dbrepo.LinkedServiceRow, error) {
	m.linkedCalls++

	return m.linked[t], m.linkedErr
}

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

func newGetter(t *testing.T, deps *mockSvcDepRepo) *Getter {
	t.Helper()

	return newGetterWithGateway(t, deps, "")
}

const testGatewayEndpoint = "https://litellm.example.com"

// newGetterWithGateway is newGetter with a configured gateway public endpoint.
func newGetterWithGateway(t *testing.T, deps *mockSvcDepRepo, endpoint string) *Getter {
	t.Helper()

	return New(deps, setupTestCatalogProvider(t), endpoint)
}

func TestGet_Local(t *testing.T) {
	id, workerID := uuid.New(), uuid.New()
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	row := &dbrepo.ModelRow{
		ID: id, DeploymentType: "local", Name: "granite-llm", ComponentType: "llm",
		Provider: "vllm-spyre", Model: "ibm-granite/granite-3.3-8b-instruct",
		Params: map[string]any{"max_len": 4096}, Status: "Running", Message: "Model running",
		WorkerID: workerID.String(), WorkerName: "lpar-1", WorkerRuntimeType: "podman",
		CreatedBy: "alice", CreatedAt: created, UpdatedAt: created,
	}

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

	got, err := newGetterWithGateway(t, deps, testGatewayEndpoint).Detail(context.Background(), row)
	require.NoError(t, err)

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
func TestGet_EndpointNotConfigured(t *testing.T) {
	for _, deploymentType := range []constants.DeploymentType{constants.DeploymentTypeLocal, constants.DeploymentTypeRemote} {
		t.Run(string(deploymentType), func(t *testing.T) {
			id := uuid.New()
			row := &dbrepo.ModelRow{ID: id, DeploymentType: deploymentType, Provider: "watsonx", CreatedBy: "alice"}

			got, err := newGetter(t, &mockSvcDepRepo{}).Detail(context.Background(), row)
			require.NoError(t, err)

			assert.Equal(t, []apimodels.ModelEndpoint{{Type: "external", URL: ""}}, got.Endpoints)
		})
	}
}

func TestGet_Remote(t *testing.T) {
	id := uuid.New()

	row := &dbrepo.ModelRow{
		ID: id, DeploymentType: "remote", Name: "prod-watsonx", ComponentType: "llm",
		Provider: "watsonx", Model: "ibm/granite-4-h-small", Status: "connected",
		Params:    map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com"},
		CreatedBy: "alice",
	}

	// A remote model has no component row: any component lookup would fail the test.
	deps := &mockSvcDepRepo{linked: map[dbmodels.DependencyType][]dbrepo.LinkedServiceRow{
		dbmodels.DependencyTypeConnector: {{ApplicationID: uuid.New(), ApplicationName: "my-rag-app"}},
		// Must be ignored: a remote model is linked as a connector, never as a component.
		dbmodels.DependencyTypeComponent: {{ApplicationID: uuid.New(), ApplicationName: "unrelated"}},
	}, counts: map[dbmodels.DependencyType]map[uuid.UUID]int{
		dbmodels.DependencyTypeConnector: {id: 1},
		dbmodels.DependencyTypeComponent: {id: 9},
	}}

	got, err := newGetterWithGateway(t, deps, testGatewayEndpoint).Detail(context.Background(), row)
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

func TestGet_NoApplicationsIsAnEmptyList(t *testing.T) {
	id := uuid.New()
	row := &dbrepo.ModelRow{ID: id, DeploymentType: "remote", Provider: "watsonx", CreatedBy: "alice"}

	got, err := newGetter(t, &mockSvcDepRepo{}).Detail(context.Background(), row)
	require.NoError(t, err)

	assert.NotNil(t, got.Applications, "applications must serialise as [] and not null")
	assert.Empty(t, got.Applications)
	assert.Zero(t, got.ConnectedServices)
}

func TestGet_FallsBackToProviderIDWhenCatalogHasNoEntry(t *testing.T) {
	id := uuid.New()
	row := &dbrepo.ModelRow{ID: id, DeploymentType: "remote", Provider: "retired-provider", CreatedBy: "alice"}

	got, err := newGetter(t, &mockSvcDepRepo{}).Detail(context.Background(), row)
	require.NoError(t, err)

	assert.Equal(t, types.ProviderInfo{ID: "retired-provider", Name: "retired-provider"}, got.Provider)
}

func TestGet_RepositoryErrors(t *testing.T) {
	row := &dbrepo.ModelRow{ID: uuid.New(), DeploymentType: "local", Provider: "vllm-spyre", CreatedBy: "alice"}
	boom := errors.New("boom")

	tests := []struct {
		name string
		deps *mockSvcDepRepo
	}{
		{"linked applications", &mockSvcDepRepo{linkedErr: boom}},
		{"service count", &mockSvcDepRepo{countErr: boom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newGetter(t, tt.deps).Detail(context.Background(), row)

			require.ErrorIs(t, err, boom)

			var valErr *validators.ValidationError
			assert.False(t, errors.As(err, &valErr), "an internal failure must not look like a client error")
		})
	}
}

func TestGet_ApplicationsOrder(t *testing.T) {
	idA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	idB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	idC := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	tests := []struct {
		name   string
		linked []dbrepo.LinkedServiceRow
		want   []apimodels.ModelApplication
	}{
		{
			name:   "sorted by name",
			linked: []dbrepo.LinkedServiceRow{{ApplicationID: idB, ApplicationName: "zeta"}, {ApplicationID: idA, ApplicationName: "alpha"}},
			want:   []apimodels.ModelApplication{{ID: idA.String(), Name: "alpha"}, {ID: idB.String(), Name: "zeta"}},
		},
		{
			name:   "applications with the same name are ordered by id",
			linked: []dbrepo.LinkedServiceRow{{ApplicationID: idC, ApplicationName: "app"}, {ApplicationID: idA, ApplicationName: "app"}, {ApplicationID: idB, ApplicationName: "app"}},
			want:   []apimodels.ModelApplication{{ID: idA.String(), Name: "app"}, {ID: idB.String(), Name: "app"}, {ID: idC.String(), Name: "app"}},
		},
		{
			name:   "an application used by several services is listed once",
			linked: []dbrepo.LinkedServiceRow{{ApplicationID: idA, ApplicationName: "app"}, {ApplicationID: idA, ApplicationName: "app"}},
			want:   []apimodels.ModelApplication{{ID: idA.String(), Name: "app"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := &dbrepo.ModelRow{ID: uuid.New(), DeploymentType: "remote", Provider: "watsonx", CreatedBy: "alice"}
			deps := &mockSvcDepRepo{linked: map[dbmodels.DependencyType][]dbrepo.LinkedServiceRow{dbmodels.DependencyTypeConnector: tt.linked}}

			got, err := newGetter(t, deps).Detail(context.Background(), row)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got.Applications)
		})
	}
}
