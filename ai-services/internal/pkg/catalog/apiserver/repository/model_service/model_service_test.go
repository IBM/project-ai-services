package modelservice

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockComponentRepo is safe for concurrent use: local deployments update it from a goroutine.
type mockComponentRepo struct {
	dbrepo.ComponentRepository
	mu         sync.Mutex
	components map[string]*dbmodels.Component
	byID       map[uuid.UUID]*dbmodels.Component
	// statuses records every status a component was moved to, in order.
	statuses map[uuid.UUID][]dbmodels.ComponentStatus
}

func newMockComponentRepo() *mockComponentRepo {
	return &mockComponentRepo{
		components: make(map[string]*dbmodels.Component),
		byID:       make(map[uuid.UUID]*dbmodels.Component),
		statuses:   make(map[uuid.UUID][]dbmodels.ComponentStatus),
	}
}

func (m *mockComponentRepo) Insert(_ context.Context, c *dbmodels.Component) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	m.components[c.Name] = c
	m.byID[c.ID] = c

	return nil
}

// get returns a snapshot of the component, or nil.
func (m *mockComponentRepo) get(id uuid.UUID) *dbmodels.Component {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.byID[id]
	if !ok {
		return nil
	}
	snapshot := *c

	return &snapshot
}

func (m *mockComponentRepo) GetByName(_ context.Context, name string) (*dbmodels.Component, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for n, c := range m.components {
		if stringsEqualFold(n, name) {
			return c, nil
		}
	}

	return nil, nil
}

func (m *mockComponentRepo) UpdateStatus(_ context.Context, id uuid.UUID, status dbmodels.ComponentStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.byID[id]; ok {
		c.Status = status
		c.Message = message
		m.statuses[id] = append(m.statuses[id], status)
	}

	return nil
}

// statusHistory returns the statuses the component was moved to after its insert.
func (m *mockComponentRepo) statusHistory(id uuid.UUID) []dbmodels.ComponentStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]dbmodels.ComponentStatus(nil), m.statuses[id]...)
}

func (m *mockComponentRepo) UpdateEndpoints(_ context.Context, id uuid.UUID, endpoints []map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.byID[id]; ok {
		c.Endpoints = endpoints
	}

	return nil
}

type mockConnectorRepo struct {
	dbrepo.ConnectorRepository
	connectors map[string]*dbmodels.Connector
}

func newMockConnectorRepo() *mockConnectorRepo {
	return &mockConnectorRepo{
		connectors: make(map[string]*dbmodels.Connector),
	}
}

func (m *mockConnectorRepo) Insert(_ context.Context, c *dbmodels.Connector) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	m.connectors[c.Name] = c

	return nil
}

func (m *mockConnectorRepo) GetByName(_ context.Context, name string) (*dbmodels.Connector, error) {
	for n, c := range m.connectors {
		if stringsEqualFold(n, name) {
			return c, nil
		}
	}

	return nil, nil
}

type mockWorkerRepo struct {
	dbrepo.WorkerRepository
	workers map[string]*dbmodels.Worker
}

func newMockWorkerRepo() *mockWorkerRepo {
	return &mockWorkerRepo{
		workers: make(map[string]*dbmodels.Worker),
	}
}

func (m *mockWorkerRepo) GetByName(_ context.Context, name string) (*dbmodels.Worker, error) {
	if w, ok := m.workers[name]; ok {
		return w, nil
	}

	return nil, nil
}

type mockLiteLLMClient struct {
	registeredModels   []catalogclient.LiteLLMModelNewRequest
	deregisteredModels []string
	registeredPass     []catalogclient.LiteLLMPassThroughRequest
	deregisteredPass   []string
	probeResponse      *catalogclient.LiteLLMHealthResponse
	probeErr           error
}

func (m *mockLiteLLMClient) RegisterModel(_ context.Context, req catalogclient.LiteLLMModelNewRequest) error {
	m.registeredModels = append(m.registeredModels, req)

	return nil
}

func (m *mockLiteLLMClient) DeregisterModel(_ context.Context, modelName string) error {
	m.deregisteredModels = append(m.deregisteredModels, modelName)

	return nil
}

func (m *mockLiteLLMClient) RegisterPassThrough(_ context.Context, req catalogclient.LiteLLMPassThroughRequest) error {
	m.registeredPass = append(m.registeredPass, req)

	return nil
}

func (m *mockLiteLLMClient) DeregisterPassThrough(_ context.Context, id string) error {
	m.deregisteredPass = append(m.deregisteredPass, id)

	return nil
}

func (m *mockLiteLLMClient) HealthProbe(_ context.Context, _ string) (*catalogclient.LiteLLMHealthResponse, error) {
	if m.probeErr != nil {
		return nil, m.probeErr
	}
	if m.probeResponse != nil {
		return m.probeResponse, nil
	}

	return &catalogclient.LiteLLMHealthResponse{
		HealthyCount:   1,
		UnhealthyCount: 0,
		HealthyEndpoints: []catalogclient.LiteLLMHealthEndpoint{
			{Model: "test-model"},
		},
	}, nil
}

const validWatsonxKey = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQR" // 44 chars

func stringsEqualFold(s1, s2 string) bool {
	return len(s1) == len(s2) && (s1 == s2 || (len(s1) > 0 && len(s2) > 0 && (s1[0]|0x20) == (s2[0]|0x20))) // simplified for test
}

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

func TestCreateModel_OpenShift_Unsupported(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	workerRepo := newMockWorkerRepo()
	catalogProvider := setupTestCatalogProvider(t)

	workerRepo.workers["Local"] = &dbmodels.Worker{
		Name:        "Local",
		RuntimeType: "openshift",
		Status:      "ready",
	}

	svc := NewModelService(
		compRepo,
		connRepo,
		workerRepo,
		nil,
		catalogProvider,
		&mockLiteLLMClient{},
	)

	req := apimodels.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "test-model",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}

	resp, err := svc.CreateModel(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	valErr, ok := err.(*validators.ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, valErr.Code)
	assert.Contains(t, valErr.Message, "models are unsupported on openshift runtime")
}

func TestCreateModel_Validation_InvalidFields(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	catalogProvider := setupTestCatalogProvider(t)

	svc := NewModelService(
		compRepo,
		connRepo,
		nil,
		nil,
		catalogProvider,
		&mockLiteLLMClient{},
	)

	// Invalid deployment_type
	req := apimodels.CreateModelRequest{
		DeploymentType: "cloud",
		ComponentType:  "llm",
		Name:           "test-model",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}
	_, err := svc.CreateModel(context.Background(), req)
	assert.Error(t, err)

	// Invalid component_type
	req.DeploymentType = "local"
	req.ComponentType = "database"
	_, err = svc.CreateModel(context.Background(), req)
	assert.Error(t, err)

	// Invalid name
	req.ComponentType = "llm"
	req.Name = "a!"
	_, err = svc.CreateModel(context.Background(), req)
	assert.Error(t, err)
}

func TestCreateModel_Conflict_Name(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	catalogProvider := setupTestCatalogProvider(t)

	// Existing component with name "existing-model"
	_ = compRepo.Insert(context.Background(), &dbmodels.Component{
		Name:   "existing-model",
		Type:   "llm",
		Status: dbmodels.ComponentStatusRunning,
	})

	svc := NewModelService(
		compRepo,
		connRepo,
		nil,
		nil,
		catalogProvider,
		&mockLiteLLMClient{},
	)

	req := apimodels.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "existing-model",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}

	_, err := svc.CreateModel(context.Background(), req)
	require.Error(t, err)
	valErr, ok := err.(*validators.ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusConflict, valErr.Code)
}

func TestCreateModel_Remote_WatsonX_Success(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	catalogProvider := setupTestCatalogProvider(t)
	mockLite := &mockLiteLLMClient{}

	svc := NewModelService(
		compRepo,
		connRepo,
		nil,
		nil,
		catalogProvider,
		mockLite,
	)

	req := apimodels.CreateModelRequest{
		DeploymentType: "remote",
		ComponentType:  "llm",
		Name:           "prod-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-4-h-small",
		Params: map[string]any{
			"api_base":   "https://us-south.ml.cloud.ibm.com",
			"project_id": "123e4567-e89b-12d3-a456-426614174000",
			"api_key":    validWatsonxKey,
		},
		CreatedBy: "admin",
	}

	resp, err := svc.CreateModel(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, apimodels.DeploymentTypeRemote, resp.DeploymentType)

	// Verify LiteLLM registration
	require.Len(t, mockLite.registeredModels, 1)
	assert.Equal(t, "ibm/granite-4-h-small", mockLite.registeredModels[0].ModelName)
	assert.Equal(t, "watsonx/ibm/granite-4-h-small", mockLite.registeredModels[0].LiteLLMParams["model"])
	assert.Equal(t, validWatsonxKey, mockLite.registeredModels[0].LiteLLMParams["api_key"])
	assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", mockLite.registeredModels[0].LiteLLMParams["project_id"])

	// Verify connector persisted without api_key
	conn, ok := connRepo.connectors["prod-watsonx"]
	require.True(t, ok)
	assert.Equal(t, dbmodels.ConnectorStatusConnected, conn.Status)
	assert.Equal(t, "models", conn.Type)
	assert.Equal(t, "llm", conn.Metadata["component_type"])
	assert.Equal(t, "ibm/granite-4-h-small", conn.Metadata["model"])
	params, ok := conn.Metadata["params"].(map[string]any)
	require.True(t, ok, "provider params are nested under \"params\"")
	assert.NotContains(t, params, "api_key", "credentials must never be persisted")
	assert.NotContains(t, conn.Metadata, "api_key")
	assert.NotContains(t, conn.Metadata, "api_base", "params are not stored flat")
	assert.Equal(t, "https://us-south.ml.cloud.ibm.com", params["api_base"])
	assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", params["project_id"])
}

func TestCreateModel_Remote_ProbeFailure(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	catalogProvider := setupTestCatalogProvider(t)
	mockLite := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{
			HealthyCount:   0,
			UnhealthyCount: 1,
			UnhealthyEndpoints: []catalogclient.LiteLLMHealthEndpoint{
				{Error: "invalid watsonx credentials"},
			},
		},
	}

	svc := NewModelService(
		compRepo,
		connRepo,
		nil,
		nil,
		catalogProvider,
		mockLite,
	)

	req := apimodels.CreateModelRequest{
		DeploymentType: "remote",
		ComponentType:  "llm",
		Name:           "bad-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-4-h-small",
		Params: map[string]any{
			"api_base":   "https://us-south.ml.cloud.ibm.com",
			"project_id": "123e4567-e89b-12d3-a456-426614174000",
			"api_key":    validWatsonxKey,
		},
	}

	resp, err := svc.CreateModel(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	valErr, ok := err.(*validators.ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, valErr.Code)
	assert.Contains(t, valErr.Message, "invalid watsonx credentials")

	// Verify rollback
	require.Len(t, mockLite.deregisteredModels, 1)
	assert.Equal(t, "ibm/granite-4-h-small", mockLite.deregisteredModels[0])
}

// blockingRuntime is a fake Podman runtime whose ListImages blocks until released, so the
// asynchronous deployment stays in the Deploying state while the test asserts on it.
type blockingRuntime struct {
	runtime.Runtime
	release chan struct{}
}

func (r *blockingRuntime) Type() runtimeTypes.RuntimeType { return runtimeTypes.RuntimeTypePodman }

func (r *blockingRuntime) ListImages(_ context.Context) ([]runtimeTypes.Image, error) {
	<-r.release

	return nil, errors.New("released")
}

func TestCreateModel_Local_RuntimeUnavailable(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()

	svc := NewModelService(compRepo, connRepo, nil, nil, setupTestCatalogProvider(t), &mockLiteLLMClient{})
	svc.planner.WithRuntimeResolver(func(string) (runtime.Runtime, error) {
		return nil, errors.New("worker is not connected")
	})

	resp, err := svc.CreateModel(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "local-granite",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		CreatedBy:      "test-user",
	})
	assert.Nil(t, resp)
	require.Error(t, err)

	valErr, ok := err.(*validators.ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, valErr.Code)
	assert.Contains(t, valErr.Message, "worker is not connected")
	assert.Empty(t, compRepo.components, "no component row must be inserted when the runtime cannot be resolved")
}

func TestCreateModel_Local_Success(t *testing.T) {
	compRepo := newMockComponentRepo()
	connRepo := newMockConnectorRepo()
	catalogProvider := setupTestCatalogProvider(t)
	mockLite := &mockLiteLLMClient{}

	svc := NewModelService(
		compRepo,
		connRepo,
		nil,
		nil,
		catalogProvider,
		mockLite,
	)

	rt := &blockingRuntime{release: make(chan struct{})}
	t.Cleanup(func() { close(rt.release) })
	svc.planner.WithRuntimeResolver(func(string) (runtime.Runtime, error) { return rt, nil })

	req := apimodels.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "local-granite",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		CreatedBy:      "test-user",
	}

	resp, err := svc.CreateModel(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, apimodels.DeploymentTypeLocal, resp.DeploymentType)

	comp, ok := compRepo.components["local-granite"]
	require.True(t, ok)
	assert.Equal(t, dbmodels.ComponentStatusDownloading, comp.Status, "the row starts in the download phase")
	assert.Equal(t, "test-user", comp.CreatedBy)
	assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", comp.Metadata["model"])
	assert.Contains(t, comp.Metadata, "params", "params are nested, even when empty")
}

// fakeDeployer stands in for a runtime-specific types.Deployer. egress defaults to endpoint, like
// a pod on the local worker; set it to model a remote worker reached through the private egress.
type fakeDeployer struct {
	endpoint string
	egress   string
	err      error // returned by Prepare
	startErr error // returned by Start
	panics   bool
	called   chan struct{}
	steps    []string
}

func (f *fakeDeployer) Prepare(context.Context, *types.LocalPlan) error {
	defer close(f.called)

	f.steps = append(f.steps, "prepare")

	if f.panics {
		panic("boom")
	}

	return f.err
}

func (f *fakeDeployer) Start(context.Context, *types.LocalPlan) (types.ModelEndpoints, error) {
	f.steps = append(f.steps, "start")

	egress := f.egress
	if egress == "" {
		egress = f.endpoint
	}

	return types.ModelEndpoints{Internal: f.endpoint, Egress: egress}, f.startErr
}

// podmanRuntime reports the Podman runtime type; every other method would panic if called.
type podmanRuntime struct{ runtime.Runtime }

func (podmanRuntime) Type() runtimeTypes.RuntimeType { return runtimeTypes.RuntimeTypePodman }

func newLocalService(t *testing.T, d types.Deployer, lite *mockLiteLLMClient) (*ModelService, *mockComponentRepo) {
	t.Helper()

	repo := newMockComponentRepo()
	svc := NewModelService(repo, newMockConnectorRepo(), nil, nil, setupTestCatalogProvider(t), lite)
	svc.planner.
		WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{runtimeTypes.RuntimeTypePodman: d}).
		WithRuntimeResolver(func(string) (runtime.Runtime, error) { return podmanRuntime{}, nil })

	return svc, repo
}

func localRequest() apimodels.CreateModelRequest {
	return apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeLocal,
		ComponentType:  "llm",
		Name:           "local-granite",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		CreatedBy:      "test-user",
	}
}

// awaitStatus polls until the component reaches want; the deployment runs in the background.
func awaitStatus(t *testing.T, repo *mockComponentRepo, id string, want dbmodels.ComponentStatus) *dbmodels.Component {
	t.Helper()

	cid, err := uuid.Parse(id)
	require.NoError(t, err)

	var comp *dbmodels.Component
	require.Eventually(t, func() bool {
		comp = repo.get(cid)

		return comp != nil && comp.Status == want
	}, 5*time.Second, 10*time.Millisecond, "component never reached status %q", want)

	return comp
}

func TestCreateLocal_Success_RegistersRouteAndMarksRunning(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusRunning)
	assert.Equal(t, []map[string]any{{"type": "internal", "url": "http://llm-abc:8000"}}, comp.Endpoints)

	require.Len(t, lite.registeredModels, 1)
	assert.Equal(t, "hosted_vllm/ibm-granite/granite-3.3-8b-instruct", lite.registeredModels[0].LiteLLMParams["model"])
	assert.Equal(t, "http://llm-abc:8000/v1", lite.registeredModels[0].LiteLLMParams["api_base"])
	assert.Equal(t, []string{resp.ID}, lite.registeredModels[0].LiteLLMParams["tags"])
	require.Len(t, lite.registeredPass, 1)
	assert.Equal(t, "http://llm-abc:8000/tokenize", lite.registeredPass[0].Target)
}

// The DB records the pod's own address while LiteLLM is pointed at the egress the control plane
// can reach (the two differ for a pod on a remote worker).
func TestCreateLocal_RoutesThroughEgress(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", egress: "egress.cp:8080", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusRunning)
	assert.Equal(t, []map[string]any{{"type": "internal", "url": "http://llm-abc:8000"}}, comp.Endpoints)

	require.Len(t, lite.registeredModels, 1)
	assert.Equal(t, "http://egress.cp:8080/v1", lite.registeredModels[0].LiteLLMParams["api_base"])
	require.Len(t, lite.registeredPass, 1)
	assert.Equal(t, "http://egress.cp:8080/tokenize", lite.registeredPass[0].Target)
}

func TestCreateLocal_DeployFailure_MarksError(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{err: errors.New("image pull failed"), called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "image pull failed")
	assert.Empty(t, lite.registeredModels, "no route must be registered when pods failed to deploy")
}

// A model is Downloading while its images and weights are fetched, then Deploying while the pods
// start, then Running.
func TestCreateLocal_StatusProgression(t *testing.T) {
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusRunning)

	id, err := uuid.Parse(resp.ID)
	require.NoError(t, err)
	assert.Equal(t,
		[]dbmodels.ComponentStatus{dbmodels.ComponentStatusDeploying, dbmodels.ComponentStatusRunning},
		repo.statusHistory(id),
		"the insert already recorded Downloading; the executor then moves Deploying -> Running")
}

// A failed download is an Error from the Downloading phase and never reaches Deploying.
func TestCreateLocal_PrepareFailure_NeverReachesDeploying(t *testing.T) {
	deployer := &fakeDeployer{err: errors.New("image pull failed"), called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)

	id, err := uuid.Parse(resp.ID)
	require.NoError(t, err)
	assert.Equal(t, []dbmodels.ComponentStatus{dbmodels.ComponentStatusError}, repo.statusHistory(id))
}

// Prepare (download) runs before Start (pods); a failed Prepare must never start anything.
func TestCreateLocal_PreparesBeforeStarting(t *testing.T) {
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusRunning)
	assert.Equal(t, []string{"prepare", "start"}, deployer.steps)
}

func TestCreateLocal_PrepareFailure_DoesNotStart(t *testing.T) {
	deployer := &fakeDeployer{err: errors.New("image pull failed"), called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Equal(t, []string{"prepare"}, deployer.steps, "pods must not start when the download failed")
}

func TestCreateLocal_StartFailure_MarksErrorWithoutRoute(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{startErr: errors.New("no spyre cards"), called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "no spyre cards")
	assert.Empty(t, lite.registeredModels, "no route must be registered when the pods failed to start")
}

func TestCreateLocal_Panic_MarksError(t *testing.T) {
	deployer := &fakeDeployer{panics: true, called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "panic")
}

func TestCreateLocal_ProbeFailure_RollsBackRouteAndMarksError(t *testing.T) {
	lite := &mockLiteLLMClient{probeErr: errors.New("connection refused")}
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "connection refused")
	assert.Equal(t, []string{"ibm-granite/granite-3.3-8b-instruct"}, lite.deregisteredModels)
	assert.Len(t, lite.deregisteredPass, 1)
}

// The plan phase must reject bad requests before any row exists or any background work starts.
func TestCreateLocal_PlanFailures_HaveNoSideEffects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apimodels.CreateModelRequest)
		code   int
	}{
		{"unknown provider", func(r *apimodels.CreateModelRequest) { r.ProviderID = "nope" }, http.StatusNotFound},
		{"params not matching schema", func(r *apimodels.CreateModelRequest) { r.Params = map[string]any{"bogus": "x"} }, http.StatusBadRequest},
		{"unknown worker", func(r *apimodels.CreateModelRequest) { r.WorkerSelector = "ghost" }, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployer := &fakeDeployer{called: make(chan struct{})}
			svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

			req := localRequest()
			tt.mutate(&req)

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)
			requireStatus(t, err, tt.code, "")
			assert.Empty(t, repo.components, "plan failures must not write a component row")

			select {
			case <-deployer.called:
				t.Fatal("deployer must not run when planning fails")
			default:
			}
		})
	}
}

func TestPlanLocal_UnsupportedRuntime(t *testing.T) {
	svc, repo := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})
	svc.planner.WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{}) // e.g. OpenShift: no deployer registered

	_, err := svc.CreateModel(context.Background(), localRequest())
	requireStatus(t, err, http.StatusUnprocessableEntity, "unsupported")
	assert.Empty(t, repo.components)
}

func TestPlanLocal_ValuesCarryParamsAndModel(t *testing.T) {
	svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

	req := localRequest()
	req.Params = map[string]any{"apiKey": "k"}

	plan, err := svc.planner.PlanLocal(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "k", plan.Values["apiKey"])
	assert.Equal(t, req.Model, plan.Values["model"], "the requested model must reach the templates as .Values.model")
	assert.NotEqual(t, uuid.Nil, plan.ComponentID)
}

// The component row keeps the non-sensitive params; credentials only reach the pod via the values.
func TestPlanLocal_ComponentMetadataStripsSensitive(t *testing.T) {
	svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

	req := localRequest()
	req.Profile = "small"
	req.Params = map[string]any{"apiKey": "k"}

	plan, err := svc.planner.PlanLocal(context.Background(), req)
	require.NoError(t, err)

	assert.NotContains(t, plan.ComponentMetadata, "apiKey", "params are not stored flat")
	assert.Empty(t, plan.ComponentMetadata["params"], "credentials must never be persisted")
	assert.Equal(t, req.Model, plan.ComponentMetadata["model"])
	assert.Equal(t, "small", plan.ComponentMetadata["profile"])
}

func TestPlanRemote_StripsSensitiveFromMetadata(t *testing.T) {
	svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

	plan, err := svc.planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeRemote,
		ComponentType:  "embedding",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/embed",
		Params:         map[string]any{"api_base": "http://vllm.example.com:8000/v1", "api_key": "secret"},
	})
	require.NoError(t, err)

	params, ok := plan.Metadata["params"].(map[string]any)
	require.True(t, ok, "provider params are nested under \"params\"")
	assert.NotContains(t, params, "api_key")
	assert.Equal(t, "http://vllm.example.com:8000/v1", params["api_base"])
	assert.Equal(t, "embedding", plan.Metadata["component_type"])
	assert.Equal(t, "secret", plan.Route.Params["api_key"], "the gateway still receives the credential")
	assert.Equal(t, "http://vllm.example.com:8000/tokenize", plan.Route.TokenizeURL)
}

func requireStatus(t *testing.T, err error, code int, msgContains string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}
