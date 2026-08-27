package models

import (
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// CreateDatasourceRequest is the request body for creating a new datasource connector.
type CreateDatasourceRequest struct {
	// Name is the unique human-readable label for this connector.
	// Must be 3–100 characters; only letters, digits, hyphens (-), and underscores (_) are
	// allowed. Duplicate-name detection is case-insensitive ("My-DB" and "my-db" conflict).
	Name string `json:"name" binding:"required,min=3,max=100"`
	// ProviderID identifies the provider implementation (e.g. "object_storage", "file_system").
	ProviderID string `json:"provider_id" binding:"required"`
	// Params holds the provider-specific configuration. Sensitive fields (format: "password"
	// in the JSON schema) are encrypted at rest; all other fields are stored in plain text.
	Params map[string]any `json:"params" binding:"required"`
	// CreatedBy is set from the auth context, never from the request body.
	CreatedBy string `json:"-"`
}

// CreateDatasourceResponse is the response body returned after a successful datasource creation.
type CreateDatasourceResponse struct {
	ID string `json:"id"`
}

// DatasourceProviderInfo is the provider sub-object embedded in datasource API responses.
// The name is resolved at query time via catalog.CatalogProvider.LoadConnector; the id is
// stored in the DB connectors.provider column.
type DatasourceProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConnectedServiceInfo is the service sub-object embedded in ConnectedServiceItem.
// id is the catalog_id of the owning application (e.g. "rag"); name is its resolved
// display name from catalog metadata (e.g. "Digital Assistants").
type ConnectedServiceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConnectedServiceItem is one entry in the services array of GetDatasourceResponse.
// Identity fields are sourced from the service_dependencies → services → applications DB join;
// sync fields are fetched live from the service's Digitize pod and gracefully degrade to
// "unknown" when unreachable.
type ConnectedServiceItem struct {
	// ApplicationID is the UUID of the application that owns this service.
	ApplicationID string `json:"application_id"`
	// ApplicationName is the human-readable display name of the owning application.
	ApplicationName string `json:"application_name"`
	// Service contains the catalog identity (id + resolved name) of the owning application.
	Service ConnectedServiceInfo `json:"service"`
	// SyncStatus is the current sync state sourced from the Digitize pod.
	// Set to "unknown" when the Digitize pod is unreachable.
	SyncStatus string `json:"sync_status"`
	// LastSyncAt is the ISO-8601 timestamp of the last completed sync, or null when unavailable.
	LastSyncAt *string `json:"last_sync_at"`
	// ErrMsg is populated when sync state could not be fetched (e.g. service unreachable or
	// no endpoint registered). Empty on success.
	ErrMsg string `json:"err_msg,omitempty"`
}

// GetDatasourceResponse is the response body for GET /api/v1/datasources/:id.
// It returns the full connector record with non-sensitive metadata and the list of
// connected services enriched with live Digitize sync state.
type GetDatasourceResponse struct {
	// ID is the UUID of the datasource connector.
	ID string `json:"id"`
	// Name is the unique human-readable label.
	Name string `json:"name"`
	// Type is always "datasource".
	Type string `json:"type"`
	// Provider contains the provider ID and its resolved display name.
	Provider DatasourceProviderInfo `json:"provider"`
	// Status is the current connectivity status ("connected" or "offline").
	Status string `json:"status"`
	// Message contains a human-readable description of the current status (may be empty).
	Message string `json:"message,omitempty"`
	// Metadata holds the non-sensitive configuration fields.
	// Sensitive fields (e.g. secret_access_key, private_key) are always stripped.
	Metadata map[string]any `json:"metadata"`
	// Services lists every service currently connected to this datasource,
	// enriched with live sync state from each service's Digitize pod.
	Services []ConnectedServiceItem `json:"services"`
	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the last-update timestamp.
	UpdatedAt time.Time `json:"updated_at"`
}

// DatasourceResponse is the API response for a single datasource connector.
// Sensitive credential fields are never included.
type DatasourceResponse struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	Provider          DatasourceProviderInfo `json:"provider"`
	Status            string                 `json:"status"`
	Message           string                 `json:"message,omitempty"`
	ConnectedServices int                    `json:"connected_services"`
	CreatedAt         string                 `json:"created_at"`
	UpdatedAt         string                 `json:"updated_at"`
}

// DatasourceListResponse is the paginated response for the list datasources endpoint.
// Pagination reuses types.PaginationMetadata — the single canonical pagination type.
type DatasourceListResponse struct {
	Data       []DatasourceResponse     `json:"data"`
	Pagination types.PaginationMetadata `json:"pagination"`
}

// ListDatasourcesRequest carries validated pagination and filter params for the list endpoint.
type ListDatasourcesRequest struct {
	Page     int
	PageSize int
	Status   string
	Provider string
}

// Made with Bob
