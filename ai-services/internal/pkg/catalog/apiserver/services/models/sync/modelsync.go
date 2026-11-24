package modelsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// DefaultModelSyncInterval is the default period between model sync cycles.
	DefaultModelSyncInterval = 1 * time.Minute

	// DefaultMaxConsecutiveFailures is the number of consecutive probe failures
	// after which periodic health checks are suspended until manual intervention or reset.
	DefaultMaxConsecutiveFailures = 15

	// modelSyncSkew is the initial delay before the first sync run to stagger
	// it with application and connector sync services.
	modelSyncSkew = 5 * time.Second
)

// SyncService periodically probes the LiteLLM gateway for all registered local models
// and remote model connectors, reconciling their status in the database with live health.
type SyncService struct {
	compRepo               dbrepo.ComponentRepository
	connectorRepo          dbrepo.ConnectorRepository
	catalogProvider        *catalog.CatalogProvider
	modelCatalog           helpers.ModelCatalog
	litellmClient          catalogclient.LiteLLMClient
	syncInterval           time.Duration
	maxConsecutiveFailures int
	stopChan               chan struct{}
	syncMutex              sync.Mutex
	failuresMutex          sync.Mutex
	failureCounts          map[uuid.UUID]int
}

// New creates a new model status SyncService.
// If syncInterval <= 0, DefaultModelSyncInterval is used.
// If maxConsecutiveFailures <= 0, DefaultMaxConsecutiveFailures is used.
// If litellmClient is nil, a default client is created via catalogclient.NewLiteLLMClient.
func New(
	compRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	catalogProvider *catalog.CatalogProvider,
	litellmClient catalogclient.LiteLLMClient,
	syncInterval time.Duration,
	maxConsecutiveFailures int,
) *SyncService {
	if syncInterval <= 0 {
		syncInterval = DefaultModelSyncInterval
	}

	if maxConsecutiveFailures <= 0 {
		maxConsecutiveFailures = DefaultMaxConsecutiveFailures
	}

	if litellmClient == nil {
		litellmClient = catalogclient.NewLiteLLMClient()
	}

	return &SyncService{
		compRepo:               compRepo,
		connectorRepo:          connectorRepo,
		catalogProvider:        catalogProvider,
		modelCatalog:           helpers.New(catalogProvider),
		litellmClient:          litellmClient,
		syncInterval:           syncInterval,
		maxConsecutiveFailures: maxConsecutiveFailures,
		stopChan:               make(chan struct{}),
		failureCounts:          make(map[uuid.UUID]int),
	}
}

// Start launches the background synchronization loop.
func (s *SyncService) Start(ctx context.Context) {
	go s.syncLoop(ctx)
	logger.InfolnCtx(ctx, "Model sync service started")
}

// Stop gracefully stops the synchronization loop.
func (s *SyncService) Stop(ctx context.Context) {
	close(s.stopChan)
	logger.InfolnCtx(ctx, "Model sync service stopped")
}

// ResetFailures clears the consecutive failure counter for the given model or connector ID,
// allowing health probes to resume if they were previously suspended.
func (s *SyncService) ResetFailures(id uuid.UUID) {
	s.failuresMutex.Lock()
	delete(s.failureCounts, id)
	s.failuresMutex.Unlock()
}

// syncLoop handles initial delay and periodic execution of sync cycles.
func (s *SyncService) syncLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in model sync goroutine: %v", r)
		}
	}()

	select {
	case <-time.After(modelSyncSkew):
	case <-s.stopChan:
		return
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	// Initial sync run
	s.PerformSync(ctx)

	for {
		select {
		case <-ticker.C:
			s.PerformSync(ctx)
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// PerformSync runs one full synchronization cycle sequentially over local models and remote connectors.
// If a cycle is already in progress, the call returns immediately.
func (s *SyncService) PerformSync(ctx context.Context) {
	if !s.syncMutex.TryLock() {
		logger.DebuglnCtx(ctx, "Model sync already in progress, skipping this cycle")

		return
	}
	defer s.syncMutex.Unlock()

	logger.DebuglnCtx(ctx, "Starting model sync cycle")
	s.syncLocalModels(ctx)
	s.syncRemoteConnectors(ctx)
	logger.DebuglnCtx(ctx, "Completed model sync cycle")
}

// syncLocalModels checks the health of managed local model components and updates their status.
func (s *SyncService) syncLocalModels(ctx context.Context) {
	components, err := s.compRepo.GetAll(ctx)
	if err != nil {
		logger.ErrorfCtx(ctx, "Model sync: failed to list components: %v", err)

		return
	}

	modelTypes := s.modelCatalog.ComponentTypes()

	for _, comp := range components {
		if !s.shouldSyncComponent(&comp, modelTypes) {
			continue
		}

		s.syncLocalModel(ctx, &comp)
	}
}

func (s *SyncService) shouldSyncComponent(comp *dbmodels.Component, modelTypes []string) bool {
	if comp.CreatedBy == "" || !slices.Contains(modelTypes, comp.Type) {
		return false
	}

	if comp.Status == dbmodels.ComponentStatusDeploying ||
		comp.Status == dbmodels.ComponentStatusInitializing ||
		comp.Status == dbmodels.ComponentStatusDownloading ||
		comp.Status == dbmodels.ComponentStatusDeleting {
		return false
	}

	if comp.Status != dbmodels.ComponentStatusRunning && comp.Status != dbmodels.ComponentStatusError {
		return false
	}

	return !s.isSuspended(comp.ID)
}

func (s *SyncService) syncLocalModel(ctx context.Context, comp *dbmodels.Component) {
	modelID := extractLiteLLMModelID(comp.Metadata)
	if modelID == "" {
		if comp.Status == dbmodels.ComponentStatusRunning {
			s.handleLocalFailure(ctx, comp, "missing LiteLLM model ID in metadata")
		}

		return
	}

	if err := s.probe(ctx, modelID); err != nil {
		s.handleLocalFailure(ctx, comp, err.Error())
	} else {
		s.handleLocalSuccess(ctx, comp)
	}
}

// syncRemoteConnectors checks the health of remote model connectors and updates their status.
func (s *SyncService) syncRemoteConnectors(ctx context.Context) {
	connectors, err := s.connectorRepo.List(ctx, &dbrepo.ConnectorFilters{
		Type: catalogconstants.ConnectorTypeModels,
	})
	if err != nil {
		logger.ErrorfCtx(ctx, "Model sync: failed to list remote model connectors: %v", err)

		return
	}

	for _, c := range connectors {
		if !s.shouldSyncConnector(&c) {
			continue
		}

		s.syncRemoteConnector(ctx, &c)
	}
}

func (s *SyncService) shouldSyncConnector(c *dbmodels.Connector) bool {
	if c.Status != dbmodels.ConnectorStatusConnected && c.Status != dbmodels.ConnectorStatusOffline {
		return false
	}

	return !s.isSuspended(c.ID)
}

func (s *SyncService) syncRemoteConnector(ctx context.Context, c *dbmodels.Connector) {
	full, err := s.connectorRepo.GetByID(ctx, c.ID, true)
	if err != nil {
		logger.ErrorfCtx(ctx, "Model sync: failed to load connector %s metadata: %v", c.ID, err)

		return
	}

	if full == nil {
		return
	}

	modelID := extractLiteLLMModelID(full.Metadata)
	if modelID == "" {
		if c.Status == dbmodels.ConnectorStatusConnected {
			s.handleRemoteFailure(ctx, c, "missing LiteLLM model ID in metadata")
		}

		return
	}

	if err := s.probe(ctx, modelID); err != nil {
		s.handleRemoteFailure(ctx, c, err.Error())
	} else {
		s.handleRemoteSuccess(ctx, c)
	}
}

func (s *SyncService) handleLocalSuccess(ctx context.Context, comp *dbmodels.Component) {
	s.resetFailure(comp.ID)

	if comp.Status != dbmodels.ComponentStatusRunning || comp.Message != "" {
		if err := s.compRepo.UpdateStatus(ctx, comp.ID, dbmodels.ComponentStatusRunning, ""); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update component %s status to Running: %v", comp.ID, err)
		}
	}
}

func (s *SyncService) handleLocalFailure(ctx context.Context, comp *dbmodels.Component, errMsg string) {
	count := s.incrementFailure(comp.ID)

	var message string
	if count >= s.maxConsecutiveFailures {
		message = fmt.Sprintf("Health checks suspended after %d consecutive failures — manual intervention required", s.maxConsecutiveFailures)
	} else {
		message = errMsg
	}

	if comp.Status != dbmodels.ComponentStatusError || comp.Message != message {
		if err := s.compRepo.UpdateStatus(ctx, comp.ID, dbmodels.ComponentStatusError, message); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update component %s status to Error: %v", comp.ID, err)
		}
	}
}

func (s *SyncService) handleRemoteSuccess(ctx context.Context, c *dbmodels.Connector) {
	s.resetFailure(c.ID)

	if c.Status != dbmodels.ConnectorStatusConnected || c.Message != "" {
		if err := s.connectorRepo.UpdateStatus(ctx, c.ID, dbmodels.ConnectorStatusConnected, ""); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update connector %s status to Connected: %v", c.ID, err)
		}
	}
}

func (s *SyncService) handleRemoteFailure(ctx context.Context, c *dbmodels.Connector, errMsg string) {
	count := s.incrementFailure(c.ID)

	var message string
	if count >= s.maxConsecutiveFailures {
		message = fmt.Sprintf("Health checks suspended after %d consecutive failures — manual intervention required", s.maxConsecutiveFailures)
	} else {
		message = errMsg
	}

	if c.Status != dbmodels.ConnectorStatusOffline || c.Message != message {
		if err := s.connectorRepo.UpdateStatus(ctx, c.ID, dbmodels.ConnectorStatusOffline, message); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update connector %s status to Offline: %v", c.ID, err)
		}
	}
}

func (s *SyncService) probe(ctx context.Context, litellmModelID string) error {
	resp, err := s.litellmClient.HealthProbe(ctx, litellmModelID)
	if err != nil {
		return errors.New(validators.SanitizeProbeError(err.Error()))
	}

	if resp != nil && resp.HealthyCount > 0 {
		return nil
	}

	if resp != nil && len(resp.UnhealthyEndpoints) > 0 && resp.UnhealthyEndpoints[0].Error != "" {
		return errors.New(validators.SanitizeProbeError(resp.UnhealthyEndpoints[0].Error))
	}

	return errors.New("model endpoint is unhealthy")
}

func (s *SyncService) isSuspended(id uuid.UUID) bool {
	s.failuresMutex.Lock()
	defer s.failuresMutex.Unlock()

	return s.failureCounts[id] >= s.maxConsecutiveFailures
}

func (s *SyncService) resetFailure(id uuid.UUID) {
	s.failuresMutex.Lock()
	delete(s.failureCounts, id)
	s.failuresMutex.Unlock()
}

func (s *SyncService) incrementFailure(id uuid.UUID) int {
	s.failuresMutex.Lock()
	defer s.failuresMutex.Unlock()

	s.failureCounts[id]++

	return s.failureCounts[id]
}

func extractLiteLLMModelID(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}

	section, ok := metadata[catalogconstants.MetadataKeyLiteLLM].(map[string]any)
	if !ok {
		return ""
	}

	id, _ := section[catalogconstants.MetadataKeyLiteLLMModelID].(string)

	return id
}
