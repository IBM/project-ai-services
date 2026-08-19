// Package bundle defines the service-layer types and interface for catalog bundle management.
package bundle

import (
	"context"
	"io"
	"time"
)

// BundleServiceInterface is the interface fulfilled by bundleService.
// It is the only dependency injected into BundleHandler.
type BundleServiceInterface interface {
	// ValidateBundle is the dedicated validate-only path (POST /catalog/bundles/validate).
	// Reads identity from metadata.yaml inside the archive and validates directly from the
	// archive (structure, metadata, values/schema consistency, templates, labels, annotations,
	// steps.md, and relevant file contents) without permanent extraction.
	// No DB row is written and no CatalogProvider reload is triggered.
	// Returns a ServiceValidationResult or ComponentValidationResult (both implement ValidationResult).
	ValidateBundle(ctx context.Context, file io.Reader) (ValidationResult, error)

	// ProcessBundle is the synchronous POST bundle creation path.
	// Reads minimal identity metadata from the archive, checks for a conflict
	// (catalog_type + catalog_id), validates directly from the archive, extracts to the
	// permanent directory, inserts a DB row as processing, reloads CatalogProvider, and
	// then activates the row.
	// Returns a *BundleResponse re-fetched from the DB with status "active" (201).
	ProcessBundle(ctx context.Context, file io.Reader, userID string) (*BundleResponse, error)

	// ReplaceBundle is the synchronous PUT update path.
	// Validates directly from the archive, marks the existing row processing, extracts into
	// a staging directory, renames staging into the final path, UPDATEs the existing row
	// in-place (status=active, version, name, size_bytes), reloads CatalogProvider, deletes
	// the old on-disk directory when it differs, and returns 200.
	// On failure after the status transition the DB row is marked failed.
	ReplaceBundle(ctx context.Context, existing *BundleRecord, file io.Reader, userID string) (*BundleResponse, error)

	// GetByBundleID retrieves the raw BundleRecord (service-layer view of a DB row) by its
	// string UUID.  Returns (nil, nil) when not found.
	GetByBundleID(ctx context.Context, bundleID string) (*BundleRecord, error)

	// GetBundleByID returns the full BundleResponse for a specific bundle by its string UUID.
	// Returns (nil, nil) when not found.
	GetBundleByID(ctx context.Context, bundleID string) (*BundleResponse, error)

	// DeleteBundle marks the row deleting, removes the on-disk directory, triggers
	// CatalogProvider.Reload(), and then deletes the DB row.
	DeleteBundle(ctx context.Context, existing *BundleRecord) error

	// ListBundles returns a BundleListResponse containing all bundle rows ordered by
	// created_at DESC.
	ListBundles(ctx context.Context) (*BundleListResponse, error)
}

// -----------------------------------------------------------------------
// Metadata types (returned by the internal peekMetadata helper)
// -----------------------------------------------------------------------

// BundleMetadata is the interface returned by peekMetadata.
// Each concrete type carries the scalar fields from its metadata.yaml.
//
// Adding a new catalog type (e.g. "architecture") requires:
//  1. Define a new concrete struct implementing this interface.
//  2. Add a case in parseMetadataYAML to construct it.
//  3. The rest of the pipeline (ProcessBundle, ReplaceBundle, ValidateBundle) is unchanged —
//     it calls only these methods.
type BundleMetadata interface {
	// CatalogID returns the globally unique value stored in the DB catalog_id column.
	// Services:   bare id                              e.g. "my-service"
	// Components: composite <component_type>--<id>     e.g. "llm--my-provider"
	CatalogID() string

	// CatalogType returns "service" or "component".
	CatalogType() string

	// Version returns the semantic version string.
	Version() string

	// DisplayName returns the human-readable label from the metadata.yaml `name:` field.
	DisplayName() string
}

// ServiceMetadata is the BundleMetadata implementation for catalog_type="service".
type ServiceMetadata struct {
	id          string
	version     string
	displayName string
}

func (m *ServiceMetadata) CatalogID() string   { return m.id }
func (m *ServiceMetadata) CatalogType() string { return "service" }
func (m *ServiceMetadata) Version() string     { return m.version }
func (m *ServiceMetadata) DisplayName() string { return m.displayName }

// ComponentMetadata is the BundleMetadata implementation for catalog_type="component".
// ComponentType is required and must be one of the recognised component types
// (llm, embedding, reranker, vector_store).
//
// CatalogID returns the composite "<component_type>--<id>" stored in the DB.
// The same bare id may exist under different component types; they produce different
// CatalogID() values and are stored as independent DB rows.
type ComponentMetadata struct {
	id            string
	componentType string
	version       string
	displayName   string
}

// CatalogID returns "<component_type>--<id>", e.g. "llm--my-provider".
func (m *ComponentMetadata) CatalogID() string   { return m.componentType + "--" + m.id }
func (m *ComponentMetadata) CatalogType() string { return "component" }
func (m *ComponentMetadata) Version() string     { return m.version }
func (m *ComponentMetadata) DisplayName() string { return m.displayName }

// ComponentType returns the raw component_type for this metadata.
// Callers that need it type-assert to *ComponentMetadata.
func (m *ComponentMetadata) ComponentType() string { return m.componentType }

// -----------------------------------------------------------------------
// Validation result types (returned by ValidateBundle)
// -----------------------------------------------------------------------

// ValidationResult is the interface returned by ValidateBundle and serialised as the
// 200 OK body for POST /catalog/bundles/validate.
// Concrete types: ServiceValidationResult, ComponentValidationResult.
type ValidationResult interface {
	IsValid() bool
	GetCatalogType() string
	// GetCatalogID returns the bare id for services and "<component_type>--<id>" for components.
	GetCatalogID() string
	GetVersion() string
	GetDisplayName() string
}

// ServiceValidationResult is the JSON body for a successfully validated service bundle.
type ServiceValidationResult struct {
	Valid       bool   `json:"valid"`
	CatalogType string `json:"catalog_type"`
	CatalogID   string `json:"catalog_id"`
	Version     string `json:"version"`
	Name        string `json:"name,omitempty"`
}

func (r *ServiceValidationResult) IsValid() bool          { return r.Valid }
func (r *ServiceValidationResult) GetCatalogType() string { return r.CatalogType }
func (r *ServiceValidationResult) GetCatalogID() string   { return r.CatalogID }
func (r *ServiceValidationResult) GetVersion() string     { return r.Version }
func (r *ServiceValidationResult) GetDisplayName() string { return r.Name }

// ComponentValidationResult is the JSON body for a successfully validated component bundle.
type ComponentValidationResult struct {
	Valid         bool   `json:"valid"`
	CatalogType   string `json:"catalog_type"`
	ComponentType string `json:"component_type"`
	CatalogID     string `json:"catalog_id"`
	Version       string `json:"version"`
	Name          string `json:"name,omitempty"`
}

func (r *ComponentValidationResult) IsValid() bool          { return r.Valid }
func (r *ComponentValidationResult) GetCatalogType() string { return r.CatalogType }
func (r *ComponentValidationResult) GetCatalogID() string   { return r.CatalogID }
func (r *ComponentValidationResult) GetVersion() string     { return r.Version }
func (r *ComponentValidationResult) GetDisplayName() string { return r.Name }

// ValidationError carries an HTTP status code alongside its error message.
// It is returned by ValidateBundle, ProcessBundle, and ReplaceBundle so that
// BundleHandler can map it directly to the appropriate HTTP status code.
type ValidationError struct {
	Code    int
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// -----------------------------------------------------------------------
// Service-layer record and response types
// -----------------------------------------------------------------------

// BundleRecord is the service-layer view of a catalog_bundles DB row.
// It is returned by GetByBundleID and passed into ReplaceBundle / DeleteBundle.
type BundleRecord struct {
	ID          string
	Name        string
	Status      string
	CatalogType string
	CatalogID   string
	Version     string
	CreatedBy   string
	SizeBytes   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BundleResponse is the JSON representation returned to the caller on success.
// It maps directly to the BundleResponse JSON shape documented in the proposal.
type BundleResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	SizeBytes   *int64    `json:"size_bytes,omitempty"`
	CatalogType string    `json:"catalog_type"`
	CatalogID   string    `json:"catalog_id"`
	Version     string    `json:"version"`
	CreatedBy   string    `json:"created_by,omitempty"`
}

// BundleListResponse is the JSON wrapper for the list endpoint.
type BundleListResponse struct {
	Bundles []BundleResponse `json:"bundles"`
}
