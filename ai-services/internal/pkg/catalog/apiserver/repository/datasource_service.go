package repository

import (
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	datasourceservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/datasource_service"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// NewDatasourceService creates the DatasourceService wired with all known provider testers.
// The DB_ENCRYPTION_KEY environment variable is read once at startup and injected into the
// service so that business logic is decoupled from the environment at call time.
func NewDatasourceService(
	connectorRepo dbrepo.ConnectorRepository,
	provider *catalog.CatalogProvider,
) DatasourceServiceInterface {
	validator := validators.NewConnectorValidator(provider)
	encryptionKey := os.Getenv("DB_ENCRYPTION_KEY")

	return datasourceservice.NewDatasourceService(connectorRepo, validator, encryptionKey)
}

// Made with Bob
