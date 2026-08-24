package models

import (
	"time"

	"github.com/google/uuid"
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
// immutable after creation. Any non-updatable field present in Metadata is silently ignored.
type UpdateDatasourceRequest struct {
	// Metadata holds the credential fields to update (e.g. access_key_id / secret_access_key
	// for S3, username / private_key for SSH). Structural fields are filtered out server-side.
	// Note: binding:"required" only prevents null/missing — an empty map is rejected in the
	// service layer, which also validates that at least one updatable field is present.
	Metadata map[string]any `json:"metadata" binding:"required"`
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
// Sensitive credential fields are never included; Metadata is stripped of sensitive keys before use.
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

// Made with Bob
