package repository

import (
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	modelservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/model_service"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// NewModelService creates a new ModelServiceInterface instance.
func NewModelService(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	modelRepo dbrepo.ModelRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	workerRegistry stream.WorkerRegistry,
	catalogProvider *catalog.CatalogProvider,
) ModelServiceInterface {
	return modelservice.NewModelService(
		componentRepo,
		connectorRepo,
		modelRepo,
		svcDepRepo,
		worker.NewResolver(workerRegistry),
		catalogProvider,
	)
}
