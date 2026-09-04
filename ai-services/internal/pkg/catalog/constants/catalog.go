package constants

// Catalog path validation constants.
const (
	// MinPathPartsForArchOrService is the minimum number of path parts for architectures and services.
	MinPathPartsForArchOrService = 3
	// MinPathPartsForComponent is the minimum number of path parts for components.
	MinPathPartsForComponent = 4
)

// Catalog type constants.
const (
	// CatalogTypeArchitectures represents the architectures catalog type.
	CatalogTypeArchitectures = "architectures"
	// CatalogTypeServices represents the services catalog type.
	CatalogTypeServices = "services"
	// CatalogTypeComponents represents the components catalog type.
	CatalogTypeComponents = "components"
	// CatalogTypeConnectors represents the connectors catalog type.
	CatalogTypeConnectors = "connectors"
)

// Connector type constants.
const (
	// ConnectorTypeDatasource is the catalog connector type shared by all datasource providers.
	ConnectorTypeDatasource = "datasource"
)

// Datasource provider ID constants.
// These values must match the connector IDs defined in the catalog assets
// (assets/connectors/datasource/<id>/metadata.yaml).
const (
	// DatasourceProviderObjectStorage is the provider ID for S3-compatible object storage connectors.
	DatasourceProviderObjectStorage = "object_storage"
	// DatasourceProviderFileSystem is the provider ID for SSH/SFTP file system connectors.
	DatasourceProviderFileSystem = "file_system"
)

// Catalog name constants.
const (
	// CatalogAppName represents the catalog name.
	CatalogAppName = "ai-services"
	// CatalogAppTemplate represents the catalog template name used for loading catalog infrastructure templates.
	CatalogAppTemplate = "catalog"
	// CatalogSecretLabel represents the catalog secret name associated with Catalog Pod.
	CatalogSecretLabel = "ai-services.io/secret"
	// CatalogSecretSkipLabel represents if catalog secret associated with pod should be skipped while deletion.
	CatalogSecretSkipLabel = "ai-services.io/secret-skip-cleanup"
	// CatalogVolumeSkipLabel represents if catalog volume associated with pod should be skipped while deletion.
	CatalogVolumeSkipLabel = "ai-services.io/volume-skip-cleanup"
	// CatalogSecretName represents the catalog secret name.
	CatalogSecretName = "catalog-secret"
	// CatalogCertSecretName represents caddy cert secret name.
	CatalogCertSecretName = "catalog-caddy-cert-secret"
	// CatalogDBSecretName represents the catalog database secret name used in OpenShift deployments.
	CatalogDBSecretName = "catalog-db-secret"
	// CatalogConnectorSecretName represents the catalog connector encryption key secret name.
	CatalogConnectorSecretName = "catalog-db-encryption-secret"
	// CatalogDeploymentName represent the catalog deployment name.
	CatalogDeploymentName = "catalog-backend"
)

// Pagination constants.
const (
	// DefaultPageSize is the default number of items per page.
	DefaultPageSize = 20
	// MaxPageSize is the maximum number of items per page.
	MaxPageSize = 100
	// MinPage is the minimum page number.
	MinPage = 1
)

// Time format constants.
const (
	// RFC3339WithTimezone is the time format for API responses (ISO 8601 with timezone).
	RFC3339WithTimezone = "2006-01-02T15:04:05Z07:00"
)

// Environment variable name constants.
const (
	// DBEncryptionKeyEnv is the environment variable that holds the AES-256 key used to
	// encrypt sensitive connector credential fields at rest.
	DBEncryptionKeyEnv = "DB_ENCRYPTION_KEY"
)

// Made with Bob
