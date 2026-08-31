package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ConnectorSyncLog is a single sync log entry returned by the Digitize pod's
// GET /v1/connectors/{id}/syncs?latest=true endpoint.
type ConnectorSyncLog struct {
	Seq          int     `json:"seq"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   *string `json:"finished_at"`
	TotalFiles   int     `json:"total_files"`
	NewFiles     int     `json:"new_files"`
	RemovedFiles int     `json:"removed_files"`
	Status       string  `json:"status"`
	Error        string  `json:"error"`
}

// ConnectorSyncLogResponse is the paginated response from
// GET /v1/connectors/{id}/syncs on the Digitize pod.
type ConnectorSyncLogResponse struct {
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
	Items  []ConnectorSyncLog `json:"items"`
}

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

// ConnectedServiceInfo is the service sub-object embedded in ConnectedServiceItem.
// id is the catalog_id of the owning application (e.g. "rag"); name is its resolved
// display name from catalog metadata (e.g. "Digital Assistants").
type ConnectedServiceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConnectorSyncState holds the sync fields returned by the Digitize pod's
// GET /v1/connectors/{id} endpoint.
type ConnectorSyncState struct {
	SyncStatus string  `json:"sync_status"`
	LastSyncAt *string `json:"last_sync_at"`
}

// DigitizeConnectorItem is one entry returned by GET /v1/connectors on the Digitize pod.
// It mirrors the ConnectorListItem shape from the digitize service's models.
type DigitizeConnectorItem struct {
	ID         string  `json:"id"`
	SyncStatus string  `json:"sync_status"`
	LastSyncAt *string `json:"last_sync_at"`
	TotalFiles int     `json:"total_files"`
	Error      *string `json:"error"`
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

// ApplicationDatasourceItem is one entry in the GET /api/v1/applications/:id/datasources list.
type ApplicationDatasourceItem struct {
	// ID is the UUID of the datasource connector.
	ID string `json:"id"`
	// Name is the human-readable label of the datasource.
	Name string `json:"name"`
	// Provider contains the provider ID and its resolved display name.
	Provider DatasourceProviderInfo `json:"provider"`
	// Status is the connector's sync_status sourced live from the Digitize pod.
	// Set to "unknown" when the Digitize pod is unreachable.
	Status string `json:"status"`
	// Files is the total number of files tracked by the connector (total_files from Digitize).
	Files int `json:"files"`
	// LastSync is the ISO-8601 timestamp of the last completed sync, or null when unavailable.
	LastSync *string `json:"last_sync"`
	// Message is a human-readable description of the current sync state:
	//   syncing     → "Processing <ingested>/<new> files"
	//   up to date  → "<new> new files found"
	//   out of sync → <error from latest sync log>
	// Empty when no sync has run yet or the Digitize pod is unreachable.
	Message string `json:"message,omitempty"`
	// ErrMsg is populated when sync state could not be fetched (e.g. service unreachable or
	// no endpoint registered). Empty on success.
	ErrMsg string `json:"err_msg,omitempty"`
}

// ApplicationDatasourceListResponse is the paginated response for
// GET /api/v1/applications/:id/datasources.
type ApplicationDatasourceListResponse struct {
	Data       []ApplicationDatasourceItem `json:"data"`
	Pagination types.PaginationMetadata    `json:"pagination"`
}

// ListApplicationDatasourcesRequest carries validated pagination params for the
// application-scoped datasource list endpoint.
type ListApplicationDatasourcesRequest struct {
	ApplicationID string
	Page          int
	PageSize      int
}

// Made with Bob
