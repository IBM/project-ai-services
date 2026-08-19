package bundle

import (
	"context"
	"io"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// bundleService implements BundleServiceInterface.
type bundleService struct {
	repo repository.BundleRepository
	// TODO: add CatalogProvider reference for Reload() calls
	// catalogProvider *catalog.CatalogProvider
}

// NewBundleService creates a new bundleService.
func NewBundleService(repo repository.BundleRepository) BundleServiceInterface {
	return &bundleService{
		repo: repo,
	}
}

// ValidateBundle validates a .tar.gz archive without persisting anything.
//
// Processing steps (to be implemented):
//  1. Peek the root metadata.yaml — parse id, type, name, version (and component_type
//     for components). Return a *ValidationError{Code:400} on read failure,
//     *ValidationError{Code:422} on semantic errors.
//  2. Validate archive structure (required files present, no path traversal, etc.).
//  3. Parse root and runtime metadata.yaml files.
//  4. Check values.yaml and schema/metadata consistency.
//  5. Parse template files for syntax errors.
//  6. Verify service labels, annotations, and steps.md.
//  7. Scan relevant bundle files line-by-line.
//  8. Construct and return the appropriate ValidationResult:
//     - *ServiceValidationResult for catalog_type="service"
//     - *ComponentValidationResult for catalog_type="component"
func (s *bundleService) ValidateBundle(_ context.Context, _ io.Reader) (any, error) {
	// TODO: implement
	panic("not implemented")
}

// ProcessBundle is the synchronous POST creation path.
//
// Processing steps (to be implemented):
//  1. peekMetadata: read minimal identity fields from root metadata.yaml in the archive.
//  2. Conflict check: query BundleRepository.GetActiveByCatalogID. Return
//     *ValidationError{Code:409} if an active row already exists.
//  3. Full validation: call the same validation logic as ValidateBundle.
//  4. Extract archive to bundleDirPath(meta.CatalogType(), meta.CatalogID(), meta.Version()).
//     Strip the top-level directory; write the canonical directory.
//  5. Insert DB row via BundleRepository.Insert with status=processing, name, catalog_type,
//     catalog_id, version, created_by. Measure on-disk size at this point.
//  6. Reload CatalogProvider (CatalogProvider.Reload()).
//  7. Mark row active via BundleRepository.Update (status=active, size_bytes).
//  8. Re-fetch via BundleRepository.GetByID and return as *BundleResponse.
//     On failure after step 5: mark row failed, store error message.
func (s *bundleService) ProcessBundle(_ context.Context, _ io.Reader, _ string) (*BundleResponse, error) {
	// TODO: implement
	panic("not implemented")
}

// ReplaceBundle is the synchronous PUT update path.
//
// Processing steps (to be implemented):
//  1. peekMetadata: read minimal identity fields from the archive.
//  2. Immutability check: meta.CatalogID() and meta.CatalogType() must match existing record.
//     Return *ValidationError{Code:422} on mismatch.
//  3. Full validation: call the same validation logic as ValidateBundle.
//  4. Mark existing row processing via BundleRepository.Update.
//  5. Extract archive to a staging directory (<catalog_id>-<version>-new).
//  6. Rename staging directory into the final path (bundleDirPath).
//  7. UPDATE existing row in-place (status=active, version, name, size_bytes) via
//     BundleRepository.Update.
//  8. Reload CatalogProvider.
//  9. Delete old on-disk directory when it differs from the new final path.
//  10. Re-fetch via BundleRepository.GetByID and return as *BundleResponse.
//     On failure after step 4: mark row failed, store error message.
func (s *bundleService) ReplaceBundle(_ context.Context, _ *BundleRecord, _ io.Reader, _ string) (*BundleResponse, error) {
	// TODO: implement
	panic("not implemented")
}

// GetByBundleID retrieves the BundleRecord for the given string UUID.
// Returns (nil, nil) when not found.
func (s *bundleService) GetByBundleID(_ context.Context, _ string) (*BundleRecord, error) {
	// TODO: parse bundleID string to uuid.UUID
	// TODO: call s.repo.GetByID and map models.CatalogBundle → BundleRecord
	panic("not implemented")
}

// GetBundleByID returns the full BundleResponse for the given string UUID.
// Returns (nil, nil) when not found.
func (s *bundleService) GetBundleByID(_ context.Context, _ string) (*BundleResponse, error) {
	// TODO: parse bundleID string to uuid.UUID
	// TODO: call s.repo.GetByID and map models.CatalogBundle → BundleResponse
	panic("not implemented")
}

// DeleteBundle marks the row deleting, removes the on-disk directory, reloads
// CatalogProvider, and deletes the DB row.
//
// Processing steps (to be implemented):
//  1. Mark row deleting via BundleRepository.Update.
//  2. Delete on-disk directory: bundleDirPath(existing.CatalogType, existing.CatalogID, existing.Version).
//  3. Reload CatalogProvider.
//  4. Delete DB row via BundleRepository.Delete.
//     On failure before step 4: mark row failed.
func (s *bundleService) DeleteBundle(_ context.Context, _ *BundleRecord) error {
	// TODO: implement
	panic("not implemented")
}

// ListBundles returns all bundle rows ordered by created_at DESC.
func (s *bundleService) ListBundles(_ context.Context) (*BundleListResponse, error) {
	// TODO: call s.repo.ListAll and map []models.CatalogBundle → BundleListResponse
	panic("not implemented")
}
