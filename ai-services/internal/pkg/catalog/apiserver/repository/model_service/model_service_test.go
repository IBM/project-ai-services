package modelservice

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
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
}

func newMockComponentRepo() *mockComponentRepo {
	return &mockComponentRepo{
		components: make(map[string]*dbmodels.Component),
		byID:       make(map[uuid.UUID]*dbmodels.Component),
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
	}

	return nil
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
	assert.Equal(t, "watsonx/ibm/granite-4-h-small", mockLite.registeredModels[0].LiteLLMParams.Model)
	assert.Equal(t, validWatsonxKey, mockLite.registeredModels[0].LiteLLMParams.APIKey)

	// Verify connector persisted without api_key
	conn, ok := connRepo.connectors["prod-watsonx"]
	require.True(t, ok)
	assert.Equal(t, dbmodels.ConnectorStatusConnected, conn.Status)
	assert.Equal(t, "models", conn.Type)
	assert.Equal(t, "llm", conn.Metadata["component_type"])
	assert.Equal(t, "ibm/granite-4-h-small", conn.Metadata["model"])
	assert.NotContains(t, conn.Metadata, "api_key", "credentials must never be persisted")
	assert.Equal(t, "https://us-south.ml.cloud.ibm.com", conn.Metadata["api_base"])
	assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", conn.Metadata["project_id"])
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
	assert.Equal(t, dbmodels.ComponentStatusDeploying, comp.Status)
	assert.Equal(t, "test-user", comp.CreatedBy)
	assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", comp.Metadata["model"])
}
