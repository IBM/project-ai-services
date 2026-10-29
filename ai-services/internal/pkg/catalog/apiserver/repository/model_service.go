package repository

import (
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	modelservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/model_service"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// NewModelService creates a new ModelServiceInterface instance.
func NewModelService(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	workerRepo dbrepo.WorkerRepository,
	workerRegistry stream.WorkerRegistry,
	catalogProvider *catalog.CatalogProvider,
	litellmClient catalogclient.LiteLLMClient,
) ModelServiceInterface {
	return modelservice.NewModelService(
		componentRepo,
		connectorRepo,
		workerRepo,
		workerRegistry,
		catalogProvider,
		litellmClient,
	)
}
