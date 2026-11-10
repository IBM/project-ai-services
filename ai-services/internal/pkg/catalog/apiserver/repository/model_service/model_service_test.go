package modelservice

import (
	"context"
	"errors"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"maps"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	modelsvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
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

func (m *mockComponentRepo) UpdateMetadata(_ context.Context, id uuid.UUID, metadata map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.byID[id]; ok {
		c.Metadata = maps.Clone(metadata)
	}

	return nil
}

func (m *mockComponentRepo) GetByID(_ context.Context, id uuid.UUID) (*dbmodels.Component, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.byID[id]
	if !ok {
		return nil, nil
	}

	// Return a copy with its own metadata map, like a fresh DB read.
	snapshot := *c
	snapshot.Metadata = maps.Clone(c.Metadata)

	return &snapshot, nil
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

type mockLiteLLMClient struct {
	registeredModels   []catalogclient.LiteLLMModelNewRequest
	deregisteredModels []string
	registeredPass     []catalogclient.LiteLLMPassThroughRequest
	deregisteredPass   []string
	probeResponse      *catalogclient.LiteLLMHealthResponse
	probeErr           error
}

func (m *mockLiteLLMClient) RegisterModel(_ context.Context, req catalogclient.LiteLLMModelNewRequest) (string, error) {
	m.registeredModels = append(m.registeredModels, req)

	return "litellm-id-" + req.ModelName, nil
}

func (m *mockLiteLLMClient) DeregisterModel(_ context.Context, modelID string) error {
	m.deregisteredModels = append(m.deregisteredModels, modelID)

	return nil
}

func (m *mockLiteLLMClient) RegisterPassThrough(_ context.Context, req catalogclient.LiteLLMPassThroughRequest) error {
	m.registeredPass = append(m.registeredPass, req)

	return nil
}

func (m *mockLiteLLMClient) DeregisterPassThrough(_ context.Context, endpointID string) error {
	m.deregisteredPass = append(m.deregisteredPass, endpointID)

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

// newTestModelService builds a ModelService whose executor talks to lite instead of a real LiteLLM.
func newTestModelService(t *testing.T, comps dbrepo.ComponentRepository, conns dbrepo.ConnectorRepository, workers worker.Resolver, lite catalogclient.LiteLLMClient) *ModelService {
	t.Helper()

	svc := NewModelService(comps, conns, nil, nil, workers, setupTestCatalogProvider(t), "")
	svc.executor = modelsvc.NewModelExecutor(comps, conns, lite)

	return svc
}

// fakeWorkers is a worker.Resolver that hands out a fixed runtime (or error) and worker IDs.
type fakeWorkers struct {
	worker.Resolver
	rt  runtime.Runtime
	err error
	ids map[string]uuid.UUID
}

func (f *fakeWorkers) NewRuntime(context.Context, string, string) (runtime.Runtime, error) {
	return f.rt, f.err
}

func (f *fakeWorkers) ID(name string) (uuid.UUID, bool) {
	id, ok := f.ids[name]

	return id, ok
}

// mockWorkerRegistry reports a fixed runtime type per worker. A worker with a runtime type is
// connected.
type mockWorkerRegistry struct {
	stream.WorkerRegistry
	runtimes map[string]string
}

func (m *mockWorkerRegistry) IsWorkerConnected(_ context.Context, name string) bool {
	_, ok := m.runtimes[name]

	return ok
}

func (m *mockWorkerRegistry) WorkerRuntimeType(name string) (string, bool) {
	rt, ok := m.runtimes[name]

	return rt, ok
}

// setControlPlaneRuntime sets the global runtime factory for the duration of a test.
func setControlPlaneRuntime(t *testing.T, rt runtimeTypes.RuntimeType) {
	t.Helper()

	prev := vars.RuntimeFactory
	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	t.Cleanup(func() { vars.RuntimeFactory = prev })
}

func TestCreateModel_OpenShift_Unsupported(t *testing.T) {
	const ocpWorker = "ocp-worker"

	tests := []struct {
		name         string
		controlPlane runtimeTypes.RuntimeType
		deployment   constants.DeploymentType
		selector     string
		// registryRuntimes is the live registry content (worker name -> runtime type).
		registryRuntimes map[string]string
	}{
		{
			name:         "local model, openshift control plane",
			controlPlane: runtimeTypes.RuntimeTypeOpenShift,
			deployment:   constants.DeploymentTypeLocal,
		},
		{
			name:         "remote model, openshift control plane",
			controlPlane: runtimeTypes.RuntimeTypeOpenShift,
			deployment:   constants.DeploymentTypeRemote,
		},
		{
			name:             "default worker is openshift",
			controlPlane:     runtimeTypes.RuntimeTypePodman,
			deployment:       constants.DeploymentTypeLocal,
			registryRuntimes: map[string]string{"Local": string(runtimeTypes.RuntimeTypeOpenShift)},
		},
		{
			name:             "selected remote worker is openshift",
			controlPlane:     runtimeTypes.RuntimeTypePodman,
			deployment:       constants.DeploymentTypeLocal,
			selector:         ocpWorker,
			registryRuntimes: map[string]string{ocpWorker: string(runtimeTypes.RuntimeTypeOpenShift)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setControlPlaneRuntime(t, tt.controlPlane)

			compRepo := newMockComponentRepo()
			connRepo := newMockConnectorRepo()

			var registry stream.WorkerRegistry
			if tt.registryRuntimes != nil {
				registry = &mockWorkerRegistry{runtimes: tt.registryRuntimes}
			}

			svc := newTestModelService(t, compRepo, connRepo, worker.NewResolver(registry), &mockLiteLLMClient{})

			req := validRequest()
			req.DeploymentType = tt.deployment
			req.WorkerName = tt.selector

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, http.StatusBadRequest, valErr.Code)
			assert.Contains(t, valErr.Message, "models are unsupported on openshift runtime")
			assert.Empty(t, compRepo.components, "no component row must be written")
			assert.Empty(t, connRepo.connectors, "no connector must be written")
		})
	}
}

func TestCreateModel_Validation_InvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*apimodels.CreateModelRequest)
		wantMsg string
	}{
		{"invalid component_type", func(r *apimodels.CreateModelRequest) { r.ComponentType = "database" }, "invalid component_type"},
		{"invalid name", func(r *apimodels.CreateModelRequest) { r.Name = "a!" }, "Model name may only contain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setControlPlaneRuntime(t, runtimeTypes.RuntimeTypePodman)

			svc := newTestModelService(t, newMockComponentRepo(), newMockConnectorRepo(), worker.NewResolver(nil), &mockLiteLLMClient{})

			req := validRequest()
			tt.mutate(&req)

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, http.StatusBadRequest, valErr.Code)
			assert.Contains(t, valErr.Message, tt.wantMsg)
		})
	}
}

func TestCreateModel_Conflict_Name(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		seed     func(*mockComponentRepo, *mockConnectorRepo)
	}{
		{
			name:     "name used by a local model",
			existing: "existing-model",
			seed: func(comps *mockComponentRepo, _ *mockConnectorRepo) {
				_ = comps.Insert(context.Background(), &dbmodels.Component{
					Name:   "existing-model",
					Type:   "llm",
					Status: dbmodels.ComponentStatusRunning,
				})
			},
		},
		{
			name:     "name used by a remote model connector",
			existing: "existing-connector",
			seed: func(_ *mockComponentRepo, conns *mockConnectorRepo) {
				_ = conns.Insert(context.Background(), &dbmodels.Connector{Name: "existing-connector"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compRepo := newMockComponentRepo()
			connRepo := newMockConnectorRepo()
			tt.seed(compRepo, connRepo)

			svc := newTestModelService(t, compRepo, connRepo, worker.NewResolver(nil), &mockLiteLLMClient{})

			req := validRequest()
			req.Name = tt.existing

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)
			requireStatus(t, err, http.StatusConflict, "already exists")
		})
	}
}

func watsonxParams() map[string]any {
	return map[string]any{
		"api_base":   "https://us-south.ml.cloud.ibm.com",
		"project_id": "123e4567-e89b-12d3-a456-426614174000",
		"api_key":    validWatsonxKey,
	}
}

func TestCreateModel_Remote(t *testing.T) {
	watsonxRequest := func(name string) apimodels.CreateModelRequest {
		return apimodels.CreateModelRequest{
			DeploymentType: constants.DeploymentTypeRemote,
			ComponentType:  "llm",
			Name:           name,
			ProviderID:     "watsonx",
			Model:          "ibm/granite-4-h-small",
			Params:         watsonxParams(),
			CreatedBy:      "admin",
		}
	}

	tests := []struct {
		name     string
		req      apimodels.CreateModelRequest
		lite     *mockLiteLLMClient
		wantCode int // 0 means success
		wantMsg  string
		check    func(t *testing.T, lite *mockLiteLLMClient, conns *mockConnectorRepo)
	}{
		{
			name: "watsonx success registers the route and persists the connector without credentials",
			req:  watsonxRequest("prod-watsonx"),
			lite: &mockLiteLLMClient{},
			check: func(t *testing.T, lite *mockLiteLLMClient, conns *mockConnectorRepo) {
				require.Len(t, lite.registeredModels, 1)
				assert.Equal(t, "ibm/granite-4-h-small", lite.registeredModels[0].ModelName)
				assert.Equal(t, "watsonx/ibm/granite-4-h-small", lite.registeredModels[0].LiteLLMParams["model"])
				assert.Equal(t, validWatsonxKey, lite.registeredModels[0].LiteLLMParams["api_key"])
				assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", lite.registeredModels[0].LiteLLMParams["project_id"])

				conn, ok := conns.connectors["prod-watsonx"]
				require.True(t, ok)
				assert.Equal(t, dbmodels.ConnectorStatusConnected, conn.Status)
				assert.Equal(t, "models", conn.Type)
				assert.Equal(t, "llm", conn.Metadata["component_type"])
				assert.Equal(t, "ibm/granite-4-h-small", conn.Metadata["model"])
				assert.Equal(t, map[string]any{"model_id": "litellm-id-ibm/granite-4-h-small"}, conn.Metadata["litellm"])

				params, ok := conn.Metadata["params"].(map[string]any)
				require.True(t, ok, "provider params are nested under \"params\"")
				assert.NotContains(t, params, "api_key", "credentials must never be persisted")
				assert.NotContains(t, conn.Metadata, "api_key")
				assert.NotContains(t, conn.Metadata, "api_base", "params are not stored flat")
				assert.Equal(t, "https://us-south.ml.cloud.ibm.com", params["api_base"])
				assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", params["project_id"])
			},
		},
		{
			name: "failed probe is a 422 and rolls the route back",
			req:  watsonxRequest("bad-watsonx"),
			lite: &mockLiteLLMClient{
				probeResponse: &catalogclient.LiteLLMHealthResponse{
					UnhealthyCount: 1,
					UnhealthyEndpoints: []catalogclient.LiteLLMHealthEndpoint{
						{Error: "invalid watsonx credentials"},
					},
				},
			},
			wantCode: http.StatusUnprocessableEntity,
			wantMsg:  "endpoint is unhealthy",
			check: func(t *testing.T, lite *mockLiteLLMClient, conns *mockConnectorRepo) {
				assert.Equal(t, []string{"litellm-id-ibm/granite-4-h-small"}, lite.deregisteredModels)
				assert.Empty(t, conns.connectors, "no connector must be persisted when the probe fails")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setControlPlaneRuntime(t, runtimeTypes.RuntimeTypePodman)

			conns := newMockConnectorRepo()
			svc := newTestModelService(t, newMockComponentRepo(), conns, worker.NewResolver(nil), tt.lite)

			resp, err := svc.CreateModel(context.Background(), tt.req)
			if tt.wantCode == 0 {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.ID)
			} else {
				assert.Nil(t, resp)
				requireStatus(t, err, tt.wantCode, tt.wantMsg)
			}

			tt.check(t, tt.lite, conns)
		})
	}
}

// blockingRuntime is a fake Podman runtime whose ListImages blocks until released, so the
// asynchronous deployment stays in its initial state while the test asserts on it.
type blockingRuntime struct {
	runtime.Runtime
	release chan struct{}
}

func (r *blockingRuntime) Type() runtimeTypes.RuntimeType { return runtimeTypes.RuntimeTypePodman }

func (r *blockingRuntime) ListImages(_ context.Context) ([]runtimeTypes.Image, error) {
	<-r.release

	return nil, errors.New("released")
}

// A local request returns as soon as the component row exists, before any download happens.
func TestCreateModel_Local_InitialRow(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*apimodels.CreateModelRequest)
		wantProfile any
	}{
		{name: "minimal request", mutate: func(*apimodels.CreateModelRequest) {}},
		{name: "request with a profile", mutate: func(r *apimodels.CreateModelRequest) { r.Profile = "small" }, wantProfile: "small"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setControlPlaneRuntime(t, runtimeTypes.RuntimeTypePodman)

			rt := &blockingRuntime{release: make(chan struct{})}
			t.Cleanup(func() { close(rt.release) })

			compRepo := newMockComponentRepo()
			svc := newTestModelService(t, compRepo, newMockConnectorRepo(), &fakeWorkers{rt: rt}, &mockLiteLLMClient{})

			req := validRequest()
			req.Name = "local-granite"
			req.CreatedBy = "test-user"
			tt.mutate(&req)

			resp, err := svc.CreateModel(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.NotEmpty(t, resp.ID)

			comp, ok := compRepo.components["local-granite"]
			require.True(t, ok)
			assert.Equal(t, dbmodels.ComponentStatusDownloading, comp.Status, "the row starts in the download phase")
			assert.Equal(t, "test-user", comp.CreatedBy)
			assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", comp.Metadata["model"])
			assert.Contains(t, comp.Metadata, "params", "params are nested, even when empty")
			assert.Equal(t, tt.wantProfile, comp.Metadata["profile"])
		})
	}
}

// fakeDeployer stands in for a runtime-specific types.Deployer. egress defaults to endpoint, like
// a pod on the local worker; set it to model a remote worker reached through the private egress.
type fakeDeployer struct {
	endpoint string
	egress   string
	checkErr error // returned by Check
	err      error // returned by Prepare
	startErr error // returned by Start
	panics   bool
	called   chan struct{}
	steps    []string
}

func (f *fakeDeployer) Check(context.Context, *types.LocalPlan) error {
	return f.checkErr
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

// newLocalService builds a service whose workers all run a Podman runtime.
func newLocalService(t *testing.T, d types.Deployer, lite *mockLiteLLMClient) (*ModelService, *mockComponentRepo) {
	t.Helper()

	return newLocalServiceWith(t, d, lite, &fakeWorkers{rt: podmanRuntime{}})
}

func newLocalServiceWith(t *testing.T, d types.Deployer, lite *mockLiteLLMClient, workers worker.Resolver) (*ModelService, *mockComponentRepo) {
	t.Helper()

	setControlPlaneRuntime(t, runtimeTypes.RuntimeTypePodman)

	repo := newMockComponentRepo()
	svc := newTestModelService(t, repo, newMockConnectorRepo(), workers, lite)
	svc.planner.WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{runtimeTypes.RuntimeTypePodman: d})

	return svc, repo
}

func localRequest() apimodels.CreateModelRequest {
	return apimodels.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeLocal,
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

// The plan phase must reject bad requests before any row exists or any background work starts.
func TestCreateModel_Local_Rejected(t *testing.T) {
	tests := []struct {
		name string
		// workers is the worker resolver; nil means workers that run a Podman runtime.
		workers worker.Resolver
		// setup customises the service and the fake deployer before the request is made.
		setup    func(svc *ModelService, d *fakeDeployer)
		mutate   func(*apimodels.CreateModelRequest)
		wantCode int
		wantMsg  string
	}{
		{
			name:     "unknown provider",
			mutate:   func(r *apimodels.CreateModelRequest) { r.ProviderID = "nope" },
			wantCode: http.StatusNotFound,
		},
		{
			name:     "params not matching schema",
			mutate:   func(r *apimodels.CreateModelRequest) { r.Params = map[string]any{"bogus": "x"} },
			wantCode: http.StatusBadRequest,
		},
		{
			// Uses the real registry-backed resolver instead of the fake one.
			name:     "worker not connected",
			workers:  worker.NewResolver(&mockWorkerRegistry{}),
			mutate:   func(r *apimodels.CreateModelRequest) { r.WorkerName = "ghost" },
			wantCode: http.StatusUnprocessableEntity,
			wantMsg:  `worker "ghost" is not connected`,
		},
		{
			name:     "worker runtime cannot be resolved",
			workers:  &fakeWorkers{err: errors.New("worker is not connected")},
			wantCode: http.StatusUnprocessableEntity,
			wantMsg:  "worker is not connected",
		},
		{
			name: "runtime without a registered deployer",
			setup: func(svc *ModelService, _ *fakeDeployer) {
				svc.planner.WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{})
			},
			wantCode: http.StatusUnprocessableEntity,
			wantMsg:  "unsupported",
		},
		{
			name: "worker cannot host the model",
			setup: func(_ *ModelService, d *fakeDeployer) {
				d.checkErr = errors.New("insufficient Spyre cards: required 1, available 0")
			},
			wantCode: http.StatusUnprocessableEntity,
			wantMsg:  "insufficient Spyre cards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployer := &fakeDeployer{called: make(chan struct{})}
			lite := &mockLiteLLMClient{}
			workers := tt.workers
			if workers == nil {
				workers = &fakeWorkers{rt: podmanRuntime{}}
			}
			svc, repo := newLocalServiceWith(t, deployer, lite, workers)

			if tt.setup != nil {
				tt.setup(svc, deployer)
			}

			req := localRequest()
			if tt.mutate != nil {
				tt.mutate(&req)
			}

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)
			requireStatus(t, err, tt.wantCode, tt.wantMsg)

			assert.Empty(t, repo.components, "rejected requests must not write a component row")
			assert.Empty(t, deployer.steps, "nothing may be pulled, downloaded or started")
			assert.Empty(t, lite.registeredModels, "no route may be registered")

			select {
			case <-deployer.called:
				t.Fatal("deployer must not run when planning fails")
			default:
			}
		})
	}
}

// A local deployment runs in the background: Downloading (Prepare) -> Deploying (Start) -> Running,
// or Error from whichever phase failed. A route is registered only once the pods are up.
func TestCreateModel_Local_Lifecycle(t *testing.T) {
	const wantModel = "hosted_vllm/ibm-granite/granite-3.3-8b-instruct"

	tests := []struct {
		name     string
		deployer fakeDeployer
		lite     *mockLiteLLMClient

		wantStatus  dbmodels.ComponentStatus
		wantMessage string
		// wantEndpoints is the endpoint list stored on the row; nil skips the check.
		wantEndpoints []map[string]any
		wantSteps     []string
		// wantHistory is the status history after the insert; nil skips the check.
		wantHistory []dbmodels.ComponentStatus
		// wantLiteLLMModelID is the id saved under metadata["litellm"]["model_id"]; empty means
		// the key must be absent.
		wantLiteLLMModelID string
		// wantAPIBase is the route's api_base; empty means no route may be registered.
		wantAPIBase  string
		wantTokenize string
		// wantDeregistered indicates whether the route should be rolled back after failed registration.
		wantDeregistered     bool
		wantDeregisteredPass int
	}{
		{
			name:     "success registers a route to the pod and marks running",
			deployer: fakeDeployer{endpoint: "llm-abc:8000"},
			lite:     &mockLiteLLMClient{},

			wantStatus:         dbmodels.ComponentStatusRunning,
			wantEndpoints:      []map[string]any{{"type": "internal", "url": "http://llm-abc:8000"}},
			wantSteps:          []string{"prepare", "start"},
			wantHistory:        []dbmodels.ComponentStatus{dbmodels.ComponentStatusDeploying, dbmodels.ComponentStatusRunning},
			wantLiteLLMModelID: "litellm-id-ibm-granite/granite-3.3-8b-instruct",
			wantAPIBase:        "http://llm-abc:8000/v1",
			wantTokenize:       "http://llm-abc:8000/tokenize",
		},
		{
			// The DB records the pod's own address while LiteLLM is pointed at the egress the
			// control plane can reach (the two differ for a pod on a remote worker).
			name:     "route goes through the egress while the row keeps the pod address",
			deployer: fakeDeployer{endpoint: "llm-abc:8000", egress: "egress.cp:8080"},
			lite:     &mockLiteLLMClient{},

			wantStatus:         dbmodels.ComponentStatusRunning,
			wantEndpoints:      []map[string]any{{"type": "internal", "url": "http://llm-abc:8000"}},
			wantSteps:          []string{"prepare", "start"},
			wantLiteLLMModelID: "litellm-id-ibm-granite/granite-3.3-8b-instruct",
			wantAPIBase:        "http://egress.cp:8080/v1",
			wantTokenize:       "http://egress.cp:8080/tokenize",
		},
		{
			name:     "download failure is an error that never reaches deploying and never starts pods",
			deployer: fakeDeployer{err: errors.New("image pull failed")},
			lite:     &mockLiteLLMClient{},

			wantStatus:  dbmodels.ComponentStatusError,
			wantMessage: "image pull failed",
			wantSteps:   []string{"prepare"},
			wantHistory: []dbmodels.ComponentStatus{dbmodels.ComponentStatusError},
		},
		{
			name:     "start failure is an error without a route",
			deployer: fakeDeployer{startErr: errors.New("no spyre cards")},
			lite:     &mockLiteLLMClient{},

			wantStatus:  dbmodels.ComponentStatusError,
			wantMessage: "no spyre cards",
			wantSteps:   []string{"prepare", "start"},
		},
		{
			name:     "panic in the deployer is recovered as an error",
			deployer: fakeDeployer{panics: true},
			lite:     &mockLiteLLMClient{},

			wantStatus:  dbmodels.ComponentStatusError,
			wantMessage: "panic",
			wantSteps:   []string{"prepare"},
		},
		{
			name:     "failed probe rolls the route back and marks error",
			deployer: fakeDeployer{endpoint: "llm-abc:8000"},
			lite:     &mockLiteLLMClient{probeErr: errors.New("connection refused")},

			wantStatus:           dbmodels.ComponentStatusError,
			wantMessage:          "connection refused",
			wantSteps:            []string{"prepare", "start"},
			wantAPIBase:          "http://llm-abc:8000/v1",
			wantDeregistered:     true,
			wantDeregisteredPass: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployer := tt.deployer
			deployer.called = make(chan struct{})
			svc, repo := newLocalService(t, &deployer, tt.lite)

			resp, err := svc.CreateModel(context.Background(), localRequest())
			require.NoError(t, err)

			comp := awaitStatus(t, repo, resp.ID, tt.wantStatus)
			assert.Contains(t, comp.Message, tt.wantMessage)
			assert.Equal(t, tt.wantSteps, deployer.steps)

			if tt.wantEndpoints != nil {
				assert.Equal(t, tt.wantEndpoints, comp.Endpoints)
			}

			if tt.wantHistory != nil {
				id, err := uuid.Parse(resp.ID)
				require.NoError(t, err)
				assert.Equal(t, tt.wantHistory, repo.statusHistory(id),
					"the insert already recorded Downloading; the executor records the later phases")
			}

			// The id is added next to the metadata written at insert, never in place of it.
			if tt.wantLiteLLMModelID == "" {
				assert.NotContains(t, comp.Metadata, "litellm")
			} else {
				assert.Equal(t, map[string]any{"model_id": tt.wantLiteLLMModelID}, comp.Metadata["litellm"])
				assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", comp.Metadata["model"], "original metadata is kept")
				assert.Contains(t, comp.Metadata, "params", "original metadata is kept")
			}

			if tt.wantAPIBase == "" {
				assert.Empty(t, tt.lite.registeredModels, "no route may be registered")
			} else {
				require.Len(t, tt.lite.registeredModels, 1)
				assert.Equal(t, wantModel, tt.lite.registeredModels[0].LiteLLMParams["model"])
				assert.Equal(t, tt.wantAPIBase, tt.lite.registeredModels[0].LiteLLMParams["api_base"])
				assert.Equal(t, []string{resp.ID}, tt.lite.registeredModels[0].LiteLLMParams["tags"])
			}

			// The tokenize passthrough is registered only after the health probe succeeds.
			if tt.wantTokenize == "" {
				assert.Empty(t, tt.lite.registeredPass, "no tokenize passthrough may be registered")
			} else {
				require.Len(t, tt.lite.registeredPass, 1)
				assert.Equal(t, tt.wantTokenize, tt.lite.registeredPass[0].Target)
			}

			if tt.wantDeregistered {
				assert.Equal(t, []string{"litellm-id-ibm-granite/granite-3.3-8b-instruct"}, tt.lite.deregisteredModels)
			} else {
				assert.Empty(t, tt.lite.deregisteredModels)
			}
			assert.Len(t, tt.lite.deregisteredPass, tt.wantDeregisteredPass)
		})
	}
}

func TestPlanLocal(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apimodels.CreateModelRequest)
		check  func(t *testing.T, req apimodels.CreateModelRequest, plan *types.LocalPlan)
	}{
		{
			name:   "params and model reach the template values",
			mutate: func(r *apimodels.CreateModelRequest) { r.Params = map[string]any{"apiKey": "k"} },
			check: func(t *testing.T, req apimodels.CreateModelRequest, plan *types.LocalPlan) {
				assert.Equal(t, "k", plan.Values["apiKey"])
				assert.Equal(t, req.Model, plan.Values["model"], "the requested model must reach the templates as .Values.model")
				assert.NotEqual(t, uuid.Nil, plan.ComponentID)
			},
		},
		{
			// The component row keeps the non-sensitive params; credentials only reach the pod via the values.
			name: "component metadata keeps the profile and strips sensitive params",
			mutate: func(r *apimodels.CreateModelRequest) {
				r.Profile = "small"
				r.Params = map[string]any{"apiKey": "k"}
			},
			check: func(t *testing.T, req apimodels.CreateModelRequest, plan *types.LocalPlan) {
				assert.NotContains(t, plan.Metadata, "apiKey", "params are not stored flat")
				assert.Empty(t, plan.Metadata["params"], "credentials must never be persisted")
				assert.Equal(t, req.Model, plan.Metadata["model"])
				assert.Equal(t, "small", plan.Metadata["profile"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

			req := localRequest()
			tt.mutate(&req)

			plan, err := svc.planner.PlanLocal(context.Background(), req)
			require.NoError(t, err)

			tt.check(t, req, plan)
		})
	}
}

// The worker ID persisted on the component comes from the worker resolver, like the application flow.
func TestPlanLocal_WorkerID(t *testing.T) {
	workerID := uuid.New()

	tests := []struct {
		name string
		ids  map[string]uuid.UUID
		want *uuid.UUID
	}{
		{name: "registered worker", ids: map[string]uuid.UUID{"Local": workerID}, want: &workerID},
		{name: "unregistered worker", ids: map[string]uuid.UUID{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newLocalServiceWith(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{}, &fakeWorkers{rt: podmanRuntime{}, ids: tt.ids})

			plan, err := svc.planner.PlanLocal(context.Background(), localRequest())
			require.NoError(t, err)

			assert.Equal(t, "Local", plan.WorkerName)
			assert.Equal(t, tt.want, plan.WorkerID)
		})
	}
}

func TestPlanRemote(t *testing.T) {
	tests := []struct {
		name string
		req  apimodels.CreateModelRequest
		// wantParams are the non-sensitive params persisted on the connector.
		wantParams map[string]any
		// wantGateway are the params the gateway receives, credentials included.
		wantGateway      map[string]any
		wantComponent    string
		wantTokenizeURL  string
		wantNotPersisted string
	}{
		{
			name: "openai-style provider derives the tokenize target from api_base",
			req: apimodels.CreateModelRequest{
				DeploymentType: constants.DeploymentTypeRemote,
				ComponentType:  "embedding",
				Name:           "remote-vllm",
				ProviderID:     "hosted_vllm",
				Model:          "ibm/embed",
				Params:         map[string]any{"api_base": "http://vllm.example.com:8000/v1", "api_key": "secret"},
			},
			wantParams:       map[string]any{"api_base": "http://vllm.example.com:8000/v1"},
			wantGateway:      map[string]any{"api_key": "secret"},
			wantComponent:    "embedding",
			wantTokenizeURL:  "http://vllm.example.com:8000/tokenize",
			wantNotPersisted: "api_key",
		},
		{
			name: "watsonx uses its fixed tokenization endpoint",
			req: apimodels.CreateModelRequest{
				DeploymentType: constants.DeploymentTypeRemote,
				ComponentType:  "llm",
				Name:           "remote-watsonx",
				ProviderID:     "watsonx",
				Model:          "ibm/granite-4-h-small",
				Params:         watsonxParams(),
			},
			wantParams: map[string]any{
				"api_base":   "https://us-south.ml.cloud.ibm.com",
				"project_id": "123e4567-e89b-12d3-a456-426614174000",
			},
			wantGateway:      map[string]any{"api_key": validWatsonxKey},
			wantComponent:    "llm",
			wantTokenizeURL:  "http://localhost:4000/watsonx/ml/v1/text/tokenization",
			wantNotPersisted: "api_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

			plan, err := svc.planner.PlanRemote(context.Background(), tt.req)
			require.NoError(t, err)

			params, ok := plan.Metadata["params"].(map[string]any)
			require.True(t, ok, "provider params are nested under \"params\"")
			assert.NotContains(t, params, tt.wantNotPersisted)

			for k, v := range tt.wantParams {
				assert.Equal(t, v, params[k], "persisted param %q", k)
			}

			for k, v := range tt.wantGateway {
				assert.Equal(t, v, plan.Route.Params[k], "the gateway still receives %q", k)
			}

			assert.Equal(t, tt.wantComponent, plan.Metadata["component_type"])
			assert.Equal(t, tt.wantTokenizeURL, plan.Route.TokenizeURL)
		})
	}
}

func requireStatus(t *testing.T, err error, code int, msgContains string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}

// mockSvcDepRepo counts the reads made about a model.
type mockSvcDepRepo struct {
	dbrepo.ServiceDependencyRepository
	reads int
}

func (m *mockSvcDepRepo) GetServiceCountByDependency(context.Context, []uuid.UUID, dbmodels.DependencyType) (map[uuid.UUID]int, error) {
	m.reads++

	return nil, nil
}

func (m *mockSvcDepRepo) GetLinkedServiceEndpoints(context.Context, uuid.UUID, dbmodels.DependencyType) ([]dbrepo.LinkedServiceRow, error) {
	m.reads++

	return nil, nil
}

func TestGetModelByID_Ownership(t *testing.T) {
	id := uuid.New()
	row := dbrepo.ModelRow{ID: id, DeploymentType: "remote", Provider: "watsonx", CreatedBy: "alice"}

	tests := []struct {
		name      string
		rows      []dbrepo.ModelRow
		createdBy string
		wantCode  int // 0: the model is returned
	}{
		{"owner", []dbrepo.ModelRow{row}, "alice", 0},
		{"empty user skips the check", []dbrepo.ModelRow{row}, "", 0},
		{"missing", nil, "alice", http.StatusNotFound},
		{"owned by someone else", []dbrepo.ModelRow{row}, "bob", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockModelRepo{rows: tt.rows}
			deps := &mockSvcDepRepo{}
			svc := NewModelService(nil, nil, repo, deps, nil, setupTestCatalogProvider(t), "")

			got, err := svc.GetModelByID(context.Background(), id, tt.createdBy)

			assert.Equal(t, id, repo.getID)
			assert.Contains(t, repo.getTypes, "llm", "the lookup is limited to the catalog's model types")
			assert.NotContains(t, repo.getTypes, "vector_db")

			if tt.wantCode == 0 {
				require.NoError(t, err)
				assert.Equal(t, id.String(), got.ID)

				return
			}

			assert.Nil(t, got)

			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, tt.wantCode, valErr.Code)
			assert.Zero(t, deps.reads, "nothing about the model may be read")
		})
	}
}

func TestGetModelByID_RepositoryError(t *testing.T) {
	boom := errors.New("boom")
	svc := NewModelService(nil, nil, &mockModelRepo{err: boom}, &mockSvcDepRepo{}, nil, setupTestCatalogProvider(t), "")

	_, err := svc.GetModelByID(context.Background(), uuid.New(), "alice")

	require.ErrorIs(t, err, boom)

	var valErr *validators.ValidationError
	assert.False(t, errors.As(err, &valErr), "an internal failure must not look like a client error")
}
