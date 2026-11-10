package repository

import (
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	modelservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/model_service"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// NewModelService creates a new ModelServiceInterface instance.
// The MODEL_GATEWAY_PUBLIC_ENDPOINT environment variable is read once here, at startup, and may be
// unset: models are then reported with an empty endpoint url.
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
		os.Getenv(constants.ModelGatewayPublicEndpointEnv),
	)
}
