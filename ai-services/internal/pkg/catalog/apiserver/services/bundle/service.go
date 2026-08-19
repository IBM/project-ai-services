package bundle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// bundleService implements BundleServiceInterface.
type bundleService struct {
	repo repository.BundleRepository
	// TODO: add CatalogProvider reference for Reload() calls once wired in.
	// catalogProvider *catalog.CatalogProvider
}

// NewBundleService creates a new bundleService backed by the given BundleRepository.
func NewBundleService(repo repository.BundleRepository) BundleServiceInterface {
	return &bundleService{repo: repo}
}

// ValidateBundle validates a .tar.gz archive without persisting anything.
//
// Processing steps (to be implemented):
//  1. Peek the root metadata.yaml — parse id, type, name, version (and component_type
//     for components). Return *ValidationError{Code:400} on read failure,
//     *ValidationError{Code:422} on semantic errors.
//  2. Validate archive structure (required files present, no path traversal, etc.).
//  3. Parse root and runtime metadata.yaml files.
//  4. Check values.yaml and schema/metadata consistency.
//  5. Parse template files for syntax errors.
//  6. Verify service labels, annotations, and steps.md.
//  7. Scan relevant bundle files line-by-line.
//  8. Return *ServiceValidationResult or *ComponentValidationResult.
func (s *bundleService) ValidateBundle(_ context.Context, _ io.Reader) (any, error) {
	// TODO: implement
	panic("not implemented")
}

// ProcessBundle is the synchronous POST creation path.
//
//  1. peekMetadata — read minimal identity fields from the root metadata.yaml.
//  2. Conflict check — query BundleRepository.GetActiveByCatalogID; return
//     *ValidationError{Code:409} if an active row already exists.
//  3. TODO — full archive-based validation; call ValidateBundle once implemented.
//  4. Extract archive to bundleDirPath(catalogType, catalogID, version),
//     stripping the top-level directory.
//  5. Insert DB row via BundleRepository.Insert (status=processing).
//  6. TODO — CatalogProvider.Reload() once the provider reference is wired in.
//  7. Mark row active via BundleRepository.Update (status=active, size_bytes, name, version).
//  8. Re-fetch via GetBundleByID and return as *BundleResponse.
//     On failure after step 5: mark row failed and store the error message.
func (s *bundleService) ProcessBundle(ctx context.Context, file io.Reader, userID string) (*BundleResponse, error) {
	// Step 1: peek minimal identity fields from root metadata.yaml.
	archiveBytes, meta, err := peekMetadata(file)
	if err != nil {
		return nil, err
	}

	// Step 2: conflict check — return 409 if an active row already exists.
	existing, err := s.repo.GetActiveByCatalogID(ctx, meta.CatalogType(), meta.CatalogID())
	if err != nil {
		return nil, fmt.Errorf("conflict check failed: %w", err)
	}
	if existing != nil {
		return nil, &validators.ValidationError{
			Code: http.StatusConflict,
			Message: fmt.Sprintf(
				"bundle with catalog_id %q already exists (id: %s); use PUT to update",
				meta.CatalogID(), existing.ID,
			),
		}
	}

	// Step 3: TODO — full archive-based validation; call s.ValidateBundle once implemented.

	// Step 4: extract archive to the permanent bundle directory.
	destDir := bundleDirPath(meta.CatalogType(), meta.CatalogID(), meta.Version())
	sizeBytes, err := extractAndMeasure(archiveBytes, destDir)
	if err != nil {
		_ = os.RemoveAll(destDir) // best-effort cleanup of any partially-extracted files
		return nil, err
	}

	// Step 5: insert DB row with status=processing.
	row := &models.CatalogBundle{
		Name:        meta.DisplayName(),
		CatalogType: meta.CatalogType(),
		CatalogID:   meta.CatalogID(),
		Version:     meta.Version(),
		CreatedBy:   userID,
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		_ = os.RemoveAll(destDir) // best-effort cleanup of the extracted directory

		return nil, fmt.Errorf("failed to insert bundle record: %w", err)
	}

	// Step 6: TODO — CatalogProvider.Reload() once the provider reference is wired in.

	// Step 7: mark row active.
	statusActive := models.BundleStatusActive
	name := meta.DisplayName()
	version := meta.Version()
	if updateErr := s.repo.Update(ctx, row.ID, models.BundleUpdate{
		Status:    &statusActive,
		SizeBytes: &sizeBytes,
		Name:      &name,
		Version:   &version,
	}); updateErr != nil {
		s.markFailed(ctx, row.ID, updateErr.Error())

		return nil, fmt.Errorf("failed to activate bundle: %w", updateErr)
	}

	// Step 8: re-fetch the authoritative row from DB and return.
	return s.GetBundleByID(ctx, row.ID.String())
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
func (s *bundleService) GetBundleByID(ctx context.Context, bundleID string) (*BundleResponse, error) {
	id, err := uuid.Parse(bundleID)
	if err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid bundle id %q", bundleID),
		}
	}

	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundle: %w", err)
	}
	if row == nil {
		return nil, nil
	}

	return rowToResponse(row), nil
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

// -----------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------

// rowToResponse maps a DB row to the HTTP BundleResponse shape.
func rowToResponse(b *models.CatalogBundle) *BundleResponse {
	return &BundleResponse{
		ID:          b.ID.String(),
		Name:        b.Name,
		Status:      string(b.Status),
		CatalogType: b.CatalogType,
		CatalogID:   b.CatalogID,
		Version:     b.Version,
		CreatedBy:   b.CreatedBy,
		SizeBytes:   b.SizeBytes,
		CreatedAt:   b.CreatedAt,
		UpdatedAt:   b.UpdatedAt,
	}
}

// markFailed sets the row status to failed and stores the error message.
// Best-effort — any secondary error from the Update call is silently discarded.
func (s *bundleService) markFailed(ctx context.Context, id uuid.UUID, msg string) {
	statusFailed := models.BundleStatusFailed
	_ = s.repo.Update(ctx, id, models.BundleUpdate{
		Status: &statusFailed,
		Error:  &msg,
	})
}
