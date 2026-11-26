package modelsync

import (
	"context"
	"errors"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Syncer probes the LiteLLM gateway for all registered local models and remote model connectors
// and reconciles their status in the database with live health. It has no loop of its own:
// the application sync service calls Sync once per cycle, so both run at the same interval.
type Syncer struct {
	compRepo      dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	modelCatalog  helpers.ModelCatalog
	litellmClient catalogclient.LiteLLMClient
}

// New creates a Syncer.
// If litellmClient is nil, a default client is created via catalogclient.NewLiteLLMClient.
func New(
	compRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	catalogProvider *catalog.CatalogProvider,
	litellmClient catalogclient.LiteLLMClient,
) *Syncer {
	if litellmClient == nil {
		litellmClient = catalogclient.NewLiteLLMClient()
	}

	return &Syncer{
		compRepo:      compRepo,
		connectorRepo: connectorRepo,
		modelCatalog:  helpers.New(catalogProvider),
		litellmClient: litellmClient,
	}
}

// Sync runs one synchronization cycle over local models and remote model connectors.
func (s *Syncer) Sync(ctx context.Context) {
	logger.DebuglnCtx(ctx, "Starting model sync")
	s.syncLocalModels(ctx)
	s.syncRemoteConnectors(ctx)
	logger.DebuglnCtx(ctx, "Completed model sync")
}

// OwnsComponent reports whether model sync is responsible for the component's status: it is a
// model-category component registered with the LiteLLM gateway. A model component without a
// LiteLLM model ID (e.g. one deployed as part of an application) is not owned, so the application
// sync keeps checking its pods.
func (s *Syncer) OwnsComponent(comp *dbmodels.Component) bool {
	return slices.Contains(s.modelCatalog.ComponentTypes(), comp.Type) && extractLiteLLMModelID(comp.Metadata) != ""
}

// syncLocalModels checks the health of managed local model components and updates their status.
func (s *Syncer) syncLocalModels(ctx context.Context) {
	components, err := s.compRepo.GetAll(ctx)
	if err != nil {
		logger.ErrorfCtx(ctx, "Model sync: failed to list components: %v", err)

		return
	}

	for _, comp := range components {
		if !s.shouldSyncComponent(&comp) {
			continue
		}

		s.syncLocalModel(ctx, &comp)
	}
}

func (s *Syncer) shouldSyncComponent(comp *dbmodels.Component) bool {
	if comp.CreatedBy == "" || !s.OwnsComponent(comp) {
		return false
	}

	// Only Running and Error are stable, observable states; Downloading, Deploying,
	// Initializing and Deleting are transitional and owned by their own workflows.
	if comp.Status != dbmodels.ComponentStatusRunning && comp.Status != dbmodels.ComponentStatusError {
		return false
	}

	return true
}

func (s *Syncer) syncLocalModel(ctx context.Context, comp *dbmodels.Component) {
	if err := s.probe(ctx, extractLiteLLMModelID(comp.Metadata)); err != nil {
		s.handleLocalFailure(ctx, comp, err.Error())
	} else {
		s.handleLocalSuccess(ctx, comp)
	}
}

// syncRemoteConnectors checks the health of remote model connectors and updates their status.
func (s *Syncer) syncRemoteConnectors(ctx context.Context) {
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

func (s *Syncer) shouldSyncConnector(c *dbmodels.Connector) bool {
	return c.Status == dbmodels.ConnectorStatusConnected || c.Status == dbmodels.ConnectorStatusOffline
}

func (s *Syncer) syncRemoteConnector(ctx context.Context, c *dbmodels.Connector) {
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

func (s *Syncer) handleLocalSuccess(ctx context.Context, comp *dbmodels.Component) {
	if comp.Status != dbmodels.ComponentStatusRunning || comp.Message != "" {
		if err := s.compRepo.UpdateStatus(ctx, comp.ID, dbmodels.ComponentStatusRunning, ""); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update component %s status to Running: %v", comp.ID, err)
		}
	}
}

func (s *Syncer) handleLocalFailure(ctx context.Context, comp *dbmodels.Component, errMsg string) {
	if comp.Status != dbmodels.ComponentStatusError || comp.Message != errMsg {
		if err := s.compRepo.UpdateStatus(ctx, comp.ID, dbmodels.ComponentStatusError, errMsg); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update component %s status to Error: %v", comp.ID, err)
		}
	}
}

func (s *Syncer) handleRemoteSuccess(ctx context.Context, c *dbmodels.Connector) {
	if c.Status != dbmodels.ConnectorStatusConnected || c.Message != "" {
		if err := s.connectorRepo.UpdateStatus(ctx, c.ID, dbmodels.ConnectorStatusConnected, ""); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update connector %s status to Connected: %v", c.ID, err)
		}
	}
}

func (s *Syncer) handleRemoteFailure(ctx context.Context, c *dbmodels.Connector, errMsg string) {
	if c.Status != dbmodels.ConnectorStatusOffline || c.Message != errMsg {
		if err := s.connectorRepo.UpdateStatus(ctx, c.ID, dbmodels.ConnectorStatusOffline, errMsg); err != nil {
			logger.ErrorfCtx(ctx, "Model sync: failed to update connector %s status to Offline: %v", c.ID, err)
		}
	}
}

func (s *Syncer) probe(ctx context.Context, litellmModelID string) error {
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
