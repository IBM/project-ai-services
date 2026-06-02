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
)

// Catalog name constants.
const (
	// CatalogAppName represents the catalog name.
	CatalogAppName = "ai-services"
	// CatalogSecretLabel represents the catalog secret name associated with Catalog Pod.
	CatalogSecretLabel = "ai-services.io/secret"
	// CatalogSecretSkipLabel represents if catalog secret associated with pod should be skipped while deletion.
	CatalogSecretSkipLabel = "ai-services.io/secret-skip-cleanup"
	// PodmanAuthSecret represents podman auth secret name.
	PodmanAuthSecret = "podman-auth-secret"
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

// Made with Bob
