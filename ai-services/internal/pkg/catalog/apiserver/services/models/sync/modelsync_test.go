package modelsync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()
	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)
	return provider
}

type compStatusUpdate struct {
	id      uuid.UUID
	status  dbmodels.ComponentStatus
	message string
}

type mockComponentRepo struct {
	dbrepo.ComponentRepository
	mu         sync.Mutex
	components []dbmodels.Component
	getAllErr  error
	updates    []compStatusUpdate
	updateErr  error
}

func (m *mockComponentRepo) GetAll(_ context.Context) ([]dbmodels.Component, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getAllErr != nil {
		return nil, m.getAllErr
	}
	return append([]dbmodels.Component(nil), m.components...), nil
}

func (m *mockComponentRepo) UpdateStatus(_ context.Context, id uuid.UUID, status dbmodels.ComponentStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, compStatusUpdate{id: id, status: status, message: message})
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.components {
		if m.components[i].ID == id {
			m.components[i].Status = status
			m.components[i].Message = message
			break
		}
	}
	return nil
}

type connStatusUpdate struct {
	id      uuid.UUID
	status  dbmodels.ConnectorStatus
	message string
}

type mockConnectorRepo struct {
	dbrepo.ConnectorRepository
	mu         sync.Mutex
	connectors map[uuid.UUID]*dbmodels.Connector
	listErr    error
	getByIDErr error
	updates    []connStatusUpdate
	updateErr  error
}

func (m *mockConnectorRepo) List(_ context.Context, filters *dbrepo.ConnectorFilters) ([]dbmodels.Connector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var res []dbmodels.Connector
	for _, c := range m.connectors {
		if filters != nil && filters.Type != "" && c.Type != filters.Type {
			continue
		}
		// List returns non-sensitive columns (Metadata is omitted)
		res = append(res, dbmodels.Connector{
			ID:        c.ID,
			Name:      c.Name,
			Type:      c.Type,
			Provider:  c.Provider,
			Status:    c.Status,
			Message:   c.Message,
			CreatedBy: c.CreatedBy,
		})
	}
	return res, nil
}

func (m *mockConnectorRepo) GetByID(_ context.Context, id uuid.UUID, _ bool) (*dbmodels.Connector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	c, ok := m.connectors[id]
	if !ok {
		return nil, dbrepo.ErrConnectorNotFound
	}
	return c, nil
}

func (m *mockConnectorRepo) UpdateStatus(_ context.Context, id uuid.UUID, status dbmodels.ConnectorStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, connStatusUpdate{id: id, status: status, message: message})
	if m.updateErr != nil {
		return m.updateErr
	}
	if c, ok := m.connectors[id]; ok {
		c.Status = status
		c.Message = message
	}
	return nil
}

type mockLiteLLMClient struct {
	catalogclient.LiteLLMClient
	mu            sync.Mutex
	probedIDs     []string
	probeErr      error
	probeResponse *catalogclient.LiteLLMHealthResponse
	probesByModel map[string]probeResult
}

type probeResult struct {
	resp *catalogclient.LiteLLMHealthResponse
	err  error
}

func (m *mockLiteLLMClient) HealthProbe(_ context.Context, modelID string) (*catalogclient.LiteLLMHealthResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probedIDs = append(m.probedIDs, modelID)

	if m.probesByModel != nil {
		if res, ok := m.probesByModel[modelID]; ok {
			return res.resp, res.err
		}
	}
	if m.probeErr != nil {
		return nil, m.probeErr
	}
	if m.probeResponse != nil {
		return m.probeResponse, nil
	}
	return &catalogclient.LiteLLMHealthResponse{HealthyCount: 1}, nil
}

func (m *mockLiteLLMClient) ProbedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.probedIDs)
}

func makeModelMetadata(litellmModelID string) map[string]any {
	if litellmModelID == "" {
		return map[string]any{}
	}
	return map[string]any{
		catalogconstants.MetadataKeyLiteLLM: map[string]any{
			catalogconstants.MetadataKeyLiteLLMModelID: litellmModelID,
		},
	}
}

func TestSyncLocalModels_Healthy(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compID := uuid.New()
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        compID,
				Name:      "granite-model",
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  makeModelMetadata("litellm-123"),
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{HealthyCount: 1},
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 1, llmClient.ProbedCount())
	assert.Equal(t, "litellm-123", llmClient.probedIDs[0])
	assert.Empty(t, compRepo.updates)
}

func TestSyncLocalModels_Unhealthy_TransitionsToError(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compID := uuid.New()
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        compID,
				Name:      "granite-model",
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  makeModelMetadata("litellm-123"),
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{
		probeErr: errors.New("connection refused"),
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	require.Len(t, compRepo.updates, 1)
	assert.Equal(t, compID, compRepo.updates[0].id)
	assert.Equal(t, dbmodels.ComponentStatusError, compRepo.updates[0].status)
	assert.Equal(t, "Pod does not exist or connection refused", compRepo.updates[0].message)
}

func TestSyncLocalModels_Recovery_TransitionsToRunning(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compID := uuid.New()
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        compID,
				Name:      "granite-model",
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusError,
				Message:   "old error",
				Metadata:  makeModelMetadata("litellm-123"),
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{HealthyCount: 1},
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	require.Len(t, compRepo.updates, 1)
	assert.Equal(t, compID, compRepo.updates[0].id)
	assert.Equal(t, dbmodels.ComponentStatusRunning, compRepo.updates[0].status)
	assert.Empty(t, compRepo.updates[0].message)
}

func TestSyncLocalModels_SkipNonObservableAndUnmanaged(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        uuid.New(),
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusDeploying,
				Metadata:  makeModelMetadata("litellm-1"),
			},
			{
				ID:        uuid.New(),
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusInitializing,
				Metadata:  makeModelMetadata("litellm-2"),
			},
			{
				ID:        uuid.New(),
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusDownloading,
				Metadata:  makeModelMetadata("litellm-3"),
			},
			{
				ID:        uuid.New(),
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusDeleting,
				Metadata:  makeModelMetadata("litellm-4"),
			},
			{
				ID:        uuid.New(),
				Type:      "llm",
				CreatedBy: "", // Unmanaged
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  makeModelMetadata("litellm-5"),
			},
			{
				ID:        uuid.New(),
				Type:      "vector_db", // Not a model type
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  makeModelMetadata("litellm-6"),
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 0, llmClient.ProbedCount())
	assert.Empty(t, compRepo.updates)
}

func TestSyncLocalModels_MissingModelID(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compID := uuid.New()
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        compID,
				Name:      "granite-model",
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  map[string]any{}, // no litellm section
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 0, llmClient.ProbedCount())
	require.Len(t, compRepo.updates, 1)
	assert.Equal(t, compID, compRepo.updates[0].id)
	assert.Equal(t, dbmodels.ComponentStatusError, compRepo.updates[0].status)
	assert.Contains(t, compRepo.updates[0].message, "missing LiteLLM model ID")
}

func TestSyncRemoteConnectors_Healthy(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	connID := uuid.New()
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{
		connectors: map[uuid.UUID]*dbmodels.Connector{
			connID: {
				ID:        connID,
				Name:      "watsonx-model",
				Type:      catalogconstants.ConnectorTypeModels,
				Provider:  catalogconstants.ConnectorProviderWatsonx,
				Status:    dbmodels.ConnectorStatusConnected,
				Metadata:  makeModelMetadata("watsonx-litellm-1"),
				CreatedBy: "user1",
			},
		},
	}
	llmClient := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{HealthyCount: 1},
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 1, llmClient.ProbedCount())
	assert.Equal(t, "watsonx-litellm-1", llmClient.probedIDs[0])
	assert.Empty(t, connRepo.updates)
}

func TestSyncRemoteConnectors_Unhealthy_TransitionsToOffline(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	connID := uuid.New()
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{
		connectors: map[uuid.UUID]*dbmodels.Connector{
			connID: {
				ID:        connID,
				Name:      "watsonx-model",
				Type:      catalogconstants.ConnectorTypeModels,
				Provider:  catalogconstants.ConnectorProviderWatsonx,
				Status:    dbmodels.ConnectorStatusConnected,
				Metadata:  makeModelMetadata("watsonx-litellm-1"),
				CreatedBy: "user1",
			},
		},
	}
	llmClient := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{
			HealthyCount: 0,
			UnhealthyEndpoints: []catalogclient.LiteLLMHealthEndpoint{
				{Error: "invalid api key"},
			},
		},
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	require.Len(t, connRepo.updates, 1)
	assert.Equal(t, connID, connRepo.updates[0].id)
	assert.Equal(t, dbmodels.ConnectorStatusOffline, connRepo.updates[0].status)
	assert.Equal(t, "Authentication failed: invalid API credentials", connRepo.updates[0].message)
}

func TestSyncRemoteConnectors_Recovery_TransitionsToConnected(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	connID := uuid.New()
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{
		connectors: map[uuid.UUID]*dbmodels.Connector{
			connID: {
				ID:        connID,
				Name:      "watsonx-model",
				Type:      catalogconstants.ConnectorTypeModels,
				Provider:  catalogconstants.ConnectorProviderWatsonx,
				Status:    dbmodels.ConnectorStatusOffline,
				Message:   "previous failure",
				Metadata:  makeModelMetadata("watsonx-litellm-1"),
				CreatedBy: "user1",
			},
		},
	}
	llmClient := &mockLiteLLMClient{
		probeResponse: &catalogclient.LiteLLMHealthResponse{HealthyCount: 1},
	}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	require.Len(t, connRepo.updates, 1)
	assert.Equal(t, connID, connRepo.updates[0].id)
	assert.Equal(t, dbmodels.ConnectorStatusConnected, connRepo.updates[0].status)
	assert.Empty(t, connRepo.updates[0].message)
}

func TestSyncRemoteConnectors_IgnoresDatasources(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	dsID := uuid.New()
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{
		connectors: map[uuid.UUID]*dbmodels.Connector{
			dsID: {
				ID:        dsID,
				Name:      "s3-ds",
				Type:      catalogconstants.ConnectorTypeDatasource,
				Provider:  catalogconstants.DatasourceProviderObjectStorage,
				Status:    dbmodels.ConnectorStatusConnected,
				Metadata:  makeModelMetadata("litellm-s3"),
				CreatedBy: "user1",
			},
		},
	}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 0, llmClient.ProbedCount())
	assert.Empty(t, connRepo.updates)
}

func TestSyncRemoteConnectors_MissingModelID(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	connID := uuid.New()
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{
		connectors: map[uuid.UUID]*dbmodels.Connector{
			connID: {
				ID:        connID,
				Name:      "watsonx-model",
				Type:      catalogconstants.ConnectorTypeModels,
				Provider:  catalogconstants.ConnectorProviderWatsonx,
				Status:    dbmodels.ConnectorStatusConnected,
				Metadata:  map[string]any{},
				CreatedBy: "user1",
			},
		},
	}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)
	svc.PerformSync(context.Background())

	assert.Equal(t, 0, llmClient.ProbedCount())
	require.Len(t, connRepo.updates, 1)
	assert.Equal(t, connID, connRepo.updates[0].id)
	assert.Equal(t, dbmodels.ConnectorStatusOffline, connRepo.updates[0].status)
	assert.Contains(t, connRepo.updates[0].message, "missing LiteLLM model ID")
}

func TestSync_Backoff_SuspendsAfterConsecutiveFailures(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compID := uuid.New()
	compRepo := &mockComponentRepo{
		components: []dbmodels.Component{
			{
				ID:        compID,
				Name:      "granite-model",
				Type:      "llm",
				CreatedBy: "user1",
				Status:    dbmodels.ComponentStatusRunning,
				Metadata:  makeModelMetadata("litellm-123"),
			},
		},
	}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{
		probeErr: errors.New("backend timeout"),
	}

	maxFailures := 3
	svc := New(compRepo, connRepo, provider, llmClient, 0, maxFailures)

	// Run 1st failure
	svc.PerformSync(context.Background())
	assert.Equal(t, 1, llmClient.ProbedCount())
	assert.Equal(t, dbmodels.ComponentStatusError, compRepo.components[0].Status)
	assert.Contains(t, compRepo.components[0].Message, "backend timeout")

	// Run 2nd failure
	svc.PerformSync(context.Background())
	assert.Equal(t, 2, llmClient.ProbedCount())

	// Run 3rd failure — hits maxFailures limit
	svc.PerformSync(context.Background())
	assert.Equal(t, 3, llmClient.ProbedCount())
	assert.Equal(t, dbmodels.ComponentStatusError, compRepo.components[0].Status)
	assert.Equal(t, "Health checks suspended after 3 consecutive failures — manual intervention required", compRepo.components[0].Message)

	// Run 4th sync cycle — should be skipped without making a probe call
	svc.PerformSync(context.Background())
	assert.Equal(t, 3, llmClient.ProbedCount(), "probe count should not increment when suspended")

	// Manual reset of failures counter
	svc.ResetFailures(compID)

	// Run 5th sync cycle — should probe again!
	svc.PerformSync(context.Background())
	assert.Equal(t, 4, llmClient.ProbedCount(), "probe count should increment after reset")
}

func TestSync_Start_Stop(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 10*time.Millisecond, 5)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.Start(ctx)
	// Stop should close gracefully
	svc.Stop(ctx)
}

func TestSync_OverlappingSyncPrevented(t *testing.T) {
	provider := setupTestCatalogProvider(t)
	compRepo := &mockComponentRepo{}
	connRepo := &mockConnectorRepo{connectors: map[uuid.UUID]*dbmodels.Connector{}}
	llmClient := &mockLiteLLMClient{}

	svc := New(compRepo, connRepo, provider, llmClient, 0, 0)

	// Manually lock syncMutex
	svc.syncMutex.Lock()

	// PerformSync should return immediately without deadlock
	svc.PerformSync(context.Background())

	assert.Equal(t, 0, llmClient.ProbedCount())
	svc.syncMutex.Unlock()
}
