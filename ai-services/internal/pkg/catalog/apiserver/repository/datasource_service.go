package repository

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	datasourceservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/datasource_service"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// NewDatasourceService creates the DatasourceService wired with all known provider testers.
// The DB_ENCRYPTION_KEY environment variable is read once at startup and validated immediately
// so that a missing or empty key causes a fast failure before the server accepts any requests.
func NewDatasourceService(
	connectorRepo dbrepo.ConnectorRepository,
	appRepo dbrepo.ApplicationRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	provider *catalog.CatalogProvider,
) (DatasourceServiceInterface, error) {
	encryptionKey := os.Getenv(catalogconstants.DBEncryptionKeyEnv)
	if encryptionKey == "" {
		return nil, fmt.Errorf("%s environment variable must be set and non-empty", catalogconstants.DBEncryptionKeyEnv)
	}

	validator := validators.NewConnectorValidator(provider)

	return datasourceservice.NewDatasourceService(
		connectorRepo,
		appRepo,
		svcDepRepo,
		validator,
		provider,
		catalogclient.NewServiceClient(""),
		encryptionKey,
	), nil
}

// Made with Bob
