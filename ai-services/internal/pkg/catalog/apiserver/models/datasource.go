package models

import (
	"time"

	"github.com/google/uuid"
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

// UpdateDatasourceRequest is the request body for updating datasource credentials.
// Only the credential fields for the provider may be updated; structural fields are
// immutable after creation. Any non-updatable field present in Params is silently ignored.
type UpdateDatasourceRequest struct {
	// Params holds the credential fields to update. Only fields in the provider's
	// schema whose ui:section is "Authentication" may be changed; structural fields
	// are filtered out server-side.
	// Note: binding:"required" only prevents null/missing — an empty map is rejected in the
	// service layer, which also validates that at least one updatable field is present.
	Params map[string]any `json:"params" binding:"required"`
}

// PropagationError describes a single Digitize propagation failure during a datasource update.
type PropagationError struct {
	// ApplicationID is the UUID of the application whose Digitize service could not be updated.
	ApplicationID string `json:"application_id"`
	// ApplicationName is the display name of that application, for UI rendering.
	ApplicationName string `json:"application_name"`
	// Error is the human-readable reason the propagation failed.
	Error string `json:"error"`
}

// DatasourceItem is the public representation of a datasource connector returned by the API.
// Sensitive credential fields are never included.
type DatasourceItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpdateDatasourceResponse is the response body returned after a successful datasource update.
// When all Digitize propagations succeed, PropagationErrors is nil (omitted from JSON).
// When one or more propagations fail, the array is populated but the overall HTTP status is
// still 200 OK — the record was saved; only downstream propagation is partial.
type UpdateDatasourceResponse struct {
	DatasourceItem
	// PropagationErrors lists any downstream Digitize propagation failures.
	// Omitted when all propagations succeeded.
	PropagationErrors []PropagationError `json:"propagation_errors,omitempty"`
}

// DatasourceProviderInfo is the provider sub-object embedded in datasource API responses.
// The name is resolved at query time via catalog.CatalogProvider.LoadConnector; the id is
// stored in the DB connectors.provider column.
type DatasourceProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConnectorSyncState holds the sync fields returned by the downstream service pod's
// GET /v1/connectors/{id} endpoint.
type ConnectorSyncState struct {
	SyncStatus string  `json:"sync_status"`
	LastSyncAt *string `json:"last_sync_at"`
}

// ConnectedServiceItem is one entry in the services array of GetDatasourceResponse and
// DatasourceServicesResponse. Identity fields are sourced from the
// service_dependencies → services → applications DB join; sync fields are fetched live
// from the downstream service pod and gracefully degrade to "unknown" when unreachable.
type ConnectedServiceItem struct {
	// ApplicationID is the UUID of the application that owns this service.
	ApplicationID string `json:"application_id"`
	// ApplicationName is the human-readable display name of the owning application.
	ApplicationName string `json:"application_name"`
	// CatalogID is the catalog identifier of the owning application (e.g. "rag").
	CatalogID string `json:"catalog_id"`
	// Name is the resolved display name of the owning application (e.g. "Digital Assistants").
	// Falls back to CatalogID when the catalog entry cannot be loaded.
	Name string `json:"name"`
	// SyncStatus is the current sync state sourced from the downstream service pod.
	// Set to "unknown" when the pod is unreachable.
	SyncStatus string `json:"sync_status"`
	// LastSyncAt is the ISO-8601 timestamp of the last completed sync, or null when unavailable.
	LastSyncAt *string `json:"last_sync_at"`
	// ErrMsg is populated when sync state could not be fetched (e.g. service unreachable or
	// no endpoint registered). Empty on success.
	ErrMsg string `json:"err_msg,omitempty"`
}

// GetDatasourceResponse is the response body for GET /api/v1/datasources/:id.
// It returns the full connector record with non-sensitive metadata and the list of
// connected services enriched with live sync state from each downstream service pod.
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
	// enriched with live sync state from each downstream service pod.
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

// ConnectDatasourceRequest is the payload sent to the downstream Digitize service.
type ConnectDatasourceRequest struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	AllowedExtensions []string       `json:"allowed_extensions,omitempty"`
	ConnectionDetails map[string]any `json:"connection_details"`
}

// ConnectDatasourcesRequest is the request body for connecting one or more datasources to an application.
type ConnectDatasourcesRequest struct {
	// DatasourceIDs is the list of datasource connector UUIDs to connect.
	DatasourceIDs []string `json:"datasource_ids" binding:"required,min=1"`
}

// DatasourceConnectionError is a per-datasource error entry in a ConnectDatasourcesResponse.
type DatasourceConnectionError struct {
	DatasourceID string `json:"datasource_id"`
	Error        string `json:"error"`
}

// ConnectDatasourcesResponse is returned when one or more datasources fail to connect.
// On full success the handler returns 204 No Content with no body.
type ConnectDatasourcesResponse struct {
	Errors []DatasourceConnectionError `json:"errors"`
}

// DatasourceServicesResponse is the response body for GET /api/v1/datasources/:id/services.
// It returns the list of services currently connected to the datasource, enriched with live
// sync state from each downstream service pod. The services array reuses ConnectedServiceItem
// verbatim — identical shape to the services field in GetDatasourceResponse.
type DatasourceServicesResponse struct {
	// DatasourceID is the UUID of the queried datasource connector.
	DatasourceID string `json:"datasource_id"`
	// Services lists every service currently connected to this datasource,
	// enriched with live sync state from each downstream service pod.
	Services []ConnectedServiceItem `json:"services"`
}

// Made with Bob
