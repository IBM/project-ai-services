// Package bundle defines the service-layer types and interface for catalog bundle management.
package bundle

import (
	"context"
	"io"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// BundleServiceInterface is the interface fulfilled by bundleService.
// It is the only dependency injected into BundleHandler.
type BundleServiceInterface interface {
	// ValidateBundle is the dedicated validate-only path (POST /catalog/bundles/validate).
	// Reads identity from metadata.yaml inside the archive and validates directly from the
	// archive (structure, metadata, values/schema consistency, templates, labels, annotations,
	// steps.md, and relevant file contents) without permanent extraction.
	// No DB row is written and no CatalogProvider reload is triggered.
	// Returns *BundleValidationResult.
	ValidateBundle(ctx context.Context, file io.Reader) (any, error)

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
	ReplaceBundle(ctx context.Context, existing *BundleResponse, file io.Reader, userID string) (*BundleResponse, error)

	// GetBundleByID returns the full BundleResponse for a specific bundle by its UUID string.
	// Returns (nil, nil) when not found.
	GetBundleByID(ctx context.Context, bundleID string) (*BundleResponse, error)

	// DeleteBundle marks the row deleting, removes the on-disk directory, triggers
	// CatalogProvider.Reload(), and then deletes the DB row.
	DeleteBundle(ctx context.Context, existing *BundleResponse) error

	// ListBundles returns a paginated BundleListResponse ordered by created_at DESC.
	ListBundles(ctx context.Context, req BundleListRequest) (*BundleListResponse, error)
}

// BundleListRequest holds the validated pagination inputs for ListBundles.
type BundleListRequest struct {
	Page     int
	PageSize int
}

// -----------------------------------------------------------------------
// Validation result types (returned by ValidateBundle)
// -----------------------------------------------------------------------

// BundleValidationResult is the JSON body for a successfully validated bundle archive.
type BundleValidationResult struct {
	Valid       bool   `json:"valid"`
	CatalogType string `json:"catalog_type"`
	CatalogID   string `json:"catalog_id"`
	Version     string `json:"version"`
	Name        string `json:"name,omitempty"`
}

// -----------------------------------------------------------------------
// Service-layer response types
// -----------------------------------------------------------------------

// BundleResponse is the JSON representation returned to the caller on success.
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

// BundleListResponse is the paginated JSON wrapper for the list endpoint.
type BundleListResponse struct {
	Bundles    []BundleResponse         `json:"bundles"`
	Pagination types.PaginationMetadata `json:"pagination"`
}
