package models

import (
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// CreateDatasourceRequest is the request body for creating a new datasource connector.
type CreateDatasourceRequest struct {
	// Name is the unique human-readable label for this connector.
	Name string `json:"name" binding:"required,min=3,max=100"`
	// ProviderID identifies the provider implementation (e.g. "object_storage", "file_system").
	ProviderID string `json:"provider_id" binding:"required"`
	// Metadata holds the provider-specific configuration. Sensitive fields (format: "password"
	// in the JSON schema) are encrypted at rest; all other fields are stored in plain text.
	Metadata map[string]any `json:"metadata" binding:"required"`
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
