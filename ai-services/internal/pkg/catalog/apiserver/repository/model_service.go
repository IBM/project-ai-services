package repository

import (
	"context"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	modelservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/model_service"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/gateway"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// modelGatewayPublicEndpoint returns the public URL of the model gateway, read once at startup.
// On OpenShift it is derived from the live "model-gateway" Route host; on other runtimes it comes
// from the MODEL_GATEWAY_PUBLIC_ENDPOINT environment variable. It may be empty: models are then
// reported with an empty endpoint url.
func modelGatewayPublicEndpoint() string {
	if vars.RuntimeFactory != nil && vars.RuntimeFactory.GetRuntimeType() == runtimeTypes.RuntimeTypeOpenShift {
		host, err := gateway.ModelGatewayRouteHost(context.Background())
		if err != nil {
			logger.Warningf("Unable to resolve the model gateway route host: %v\n", err)

			return ""
		}

		return "https://" + host
	}

	return os.Getenv(constants.ModelGatewayPublicEndpointEnv)
}

// NewModelService creates a new ModelServiceInterface instance.
// The model gateway public endpoint is resolved once here, at startup (see modelGatewayPublicEndpoint).
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
		modelGatewayPublicEndpoint(),
	)
}
