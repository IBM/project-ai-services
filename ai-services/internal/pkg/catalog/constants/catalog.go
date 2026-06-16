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
	// CatalogAppTemplate represents the catalog template name used for loading catalog infrastructure templates.
	CatalogAppTemplate = "catalog"
	// CatalogSecretLabel represents the catalog secret name associated with Catalog Pod.
	CatalogSecretLabel = "ai-services.io/secret"
	// CatalogSecretSkipLabel represents if catalog secret associated with pod should be skipped while deletion.
	CatalogSecretSkipLabel = "ai-services.io/secret-skip-cleanup"
	// CatalogVolumeLabel represents the catalog volume name associated with Catalog Pod.
	CatalogVolumeLabel = "ai-services.io/volume"
	// CatalogVolumeSkipLabel represents if catalog volume associated with pod should be skipped while deletion.
	CatalogVolumeSkipLabel = "ai-services.io/volume-skip-cleanup"
	// PodmanAuthSecret represents podman auth secret name.
	PodmanAuthSecret = "podman-auth-secret"
	// CatalogSecretName represents the catalog secret name.
	CatalogSecretName = "catalog-secret"
	// CatalogPodmanAuthSecretName represent the podman auth secret name.
	CatalogPodmanAuthSecretName = "podman-auth-secret"
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

// Network constants.
const (
	// DefaultHTTPSPort is the default HTTPS port.
	DefaultHTTPSPort = "443"
)

// Made with Bob
