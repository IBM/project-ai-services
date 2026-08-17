package models

import (
	"time"

	"github.com/google/uuid"
)

// BundleStatus represents the lifecycle status of a catalog bundle.
type BundleStatus string

const (
	BundleStatusProcessing BundleStatus = "processing"
	BundleStatusActive     BundleStatus = "active"
	BundleStatusFailed     BundleStatus = "failed"
	BundleStatusDeleting   BundleStatus = "deleting"
)

// CatalogBundle represents a customer-created catalog bundle row in the database.
// The on-disk directory path is derived at runtime as
// /data/catalog-bundles/<catalog_type>/<catalog_id>-<version>/ and is never stored here.
type CatalogBundle struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name,omitempty"`
	Status      BundleStatus `json:"status"`
	SizeBytes   *int64       `json:"size_bytes,omitempty"`
	CatalogType string       `json:"catalog_type"`
	CatalogID   string       `json:"catalog_id"`
	Version     string       `json:"version"`
	Error       string       `json:"error,omitempty"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
