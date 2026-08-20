package bundle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// bundleService implements BundleServiceInterface.
type bundleService struct {
	repo     repository.BundleRepository
	svcRepo  repository.ServiceRepository
	compRepo repository.ComponentRepository
	// TODO: add CatalogProvider reference for Reload() calls once wired in.
	// catalogProvider *catalog.CatalogProvider
}

// NewBundleService creates a new bundleService backed by the given repositories.
func NewBundleService(repo repository.BundleRepository, svcRepo repository.ServiceRepository, compRepo repository.ComponentRepository) BundleServiceInterface {
	return &bundleService{repo: repo, svcRepo: svcRepo, compRepo: compRepo}
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
//  1. peekMetadata: read minimal identity fields from the archive.
//  2. Immutability check: meta.CatalogID() and meta.CatalogType() must match existing record.
//     Returns *ValidationError{Code:422} on mismatch.
//  3. TODO — full archive-based validation; call ValidateBundle once implemented.
//  4. Mark existing row processing via BundleRepository.Update.
//  5. Extract archive to a staging directory (<catalog_id>-<version>-new).
//  6. Rename staging directory into the final path (bundleDirPath).
//  7. UPDATE existing row in-place (status=active, version, name, size_bytes) via BundleRepository.Update.
//  8. TODO — CatalogProvider.Reload() once the provider reference is wired in.
//  9. Delete old on-disk directory when it differs from the new final path.
//  10. Re-fetch via BundleRepository.GetByID and return as *BundleResponse.
//     On failure after step 4: mark row failed, store error message.
func (s *bundleService) ReplaceBundle(ctx context.Context, existing *BundleResponse, file io.Reader, _ string) (*BundleResponse, error) {
	// Step 1: peek minimal identity fields from root metadata.yaml.
	archiveBytes, meta, err := peekMetadata(file)
	if err != nil {
		return nil, err
	}

	// Step 2: immutability check — catalog_id and catalog_type must not change.
	if meta.CatalogID() != existing.CatalogID {
		return nil, &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"catalog_id mismatch: archive contains %q but existing bundle has %q",
				meta.CatalogID(), existing.CatalogID,
			),
		}
	}
	if meta.CatalogType() != existing.CatalogType {
		return nil, &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"catalog_type mismatch: archive contains %q but existing bundle has %q",
				meta.CatalogType(), existing.CatalogType,
			),
		}
	}

	// Step 3: TODO — full archive-based validation; call s.ValidateBundle once implemented.

	// Step 3a: guard — reject if any running service/component is using this catalog entry.
	if err := s.checkNoRunningInstances(ctx, meta); err != nil {
		return nil, err
	}

	existingID, err := uuid.Parse(existing.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid existing bundle id %q: %w", existing.ID, err)
	}

	// Step 4: mark existing row processing.
	statusProcessing := models.BundleStatusProcessing
	if updateErr := s.repo.Update(ctx, existingID, models.BundleUpdate{Status: &statusProcessing}); updateErr != nil {
		return nil, fmt.Errorf("failed to mark bundle as processing: %w", updateErr)
	}

	// Steps 5–10: extract, rename, activate, cleanup. Any failure after step 4
	// marks the row failed.
	resp, replaceErr := s.replaceBundleFiles(ctx, existingID, existing, meta, archiveBytes)
	if replaceErr != nil {
		s.markFailed(ctx, existingID, replaceErr.Error())

		return nil, replaceErr
	}

	return resp, nil
}

// replaceBundleFiles performs the file-system and DB operations for ReplaceBundle
// after the row has been moved to "processing". Called only by ReplaceBundle.
func (s *bundleService) replaceBundleFiles(ctx context.Context, existingID uuid.UUID, existing *BundleResponse, meta BundleMetadata, archiveBytes []byte) (*BundleResponse, error) {
	oldDir := bundleDirPath(existing.CatalogType, existing.CatalogID, existing.Version)
	newFinalDir := bundleDirPath(meta.CatalogType(), meta.CatalogID(), meta.Version())
	stagingDir := newFinalDir + "-new"

	// Step 5: extract to staging directory.
	sizeBytes, err := extractAndMeasure(archiveBytes, stagingDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)

		return nil, err
	}

	// Step 6: rename staging into the final path.
	// Remove any leftover final directory (can exist when version is unchanged).
	_ = os.RemoveAll(newFinalDir)
	if err := os.Rename(stagingDir, newFinalDir); err != nil {
		_ = os.RemoveAll(stagingDir)

		return nil, fmt.Errorf("failed to rename staging directory into place: %w", err)
	}

	// Step 7: update DB row in-place.
	statusActive := models.BundleStatusActive
	name := meta.DisplayName()
	version := meta.Version()
	if updateErr := s.repo.Update(ctx, existingID, models.BundleUpdate{
		Status:    &statusActive,
		SizeBytes: &sizeBytes,
		Name:      &name,
		Version:   &version,
	}); updateErr != nil {
		return nil, fmt.Errorf("failed to activate replaced bundle: %w", updateErr)
	}

	// Step 8: TODO — CatalogProvider.Reload() once the provider reference is wired in.

	// Step 9: delete old on-disk directory when it differs from the new final path.
	if oldDir != newFinalDir {
		_ = os.RemoveAll(oldDir)
	}

	// Step 10: re-fetch the authoritative row from DB and return.
	return s.GetBundleByID(ctx, existingID.String())
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

// ListBundles returns one page of bundle rows ordered by created_at DESC.
// The pattern mirrors ListApplications: guard inputs, call GetCount, call GetAll with filters,
// build pagination metadata.
func (s *bundleService) ListBundles(ctx context.Context, req BundleListRequest) (*BundleListResponse, error) {
	if req.Page < 1 {
		return nil, fmt.Errorf("page must be greater than 0")
	}
	if req.PageSize < 1 {
		return nil, fmt.Errorf("pageSize must be greater than 0")
	}

	filters := &repository.BundleFilters{
		Limit:  req.PageSize,
		Offset: (req.Page - 1) * req.PageSize,
	}

	totalCount, err := s.repo.GetCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundle count: %w", err)
	}

	rows, err := s.repo.GetAll(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve bundles: %w", err)
	}

	bundles := make([]BundleResponse, 0, len(rows))
	for i := range rows {
		bundles = append(bundles, *rowToResponse(&rows[i]))
	}

	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + req.PageSize - 1) / req.PageSize
	}

	return &BundleListResponse{
		Bundles: bundles,
		Pagination: catalogtypes.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: totalCount,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}, nil
}

// -----------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------

// checkNoRunningInstances returns a 409 Conflict ValidationError when any
// service or component row in the DB is already using the catalog entry
// identified by meta.  It is called before ReplaceBundle (and can be reused
// by DeleteBundle in the future) to prevent in-flight replacements or
// deletions while live workloads depend on the bundle.
//
//   - For a service bundle:  checks the services table by catalog_id.
//   - For a component bundle: the catalog_id is "<type>--<provider>"; both
//     halves are matched against the components table (type + provider).
func (s *bundleService) checkNoRunningInstances(ctx context.Context, meta BundleMetadata) error {
	switch meta.CatalogType() {
	case CatalogTypeService:
		exists, err := s.svcRepo.ExistsByCatalogID(ctx, meta.CatalogID())
		if err != nil {
			return fmt.Errorf("failed to check running services: %w", err)
		}
		if exists {
			return &validators.ValidationError{
				Code: http.StatusConflict,
				Message: fmt.Sprintf(
					"cannot replace bundle: one or more services are currently running with catalog_id %q",
					meta.CatalogID(),
				),
			}
		}

	case CatalogTypeComponent:
		// catalog_id is "<component_type>--<provider>"; split on the first "--".
		parts := strings.SplitN(meta.CatalogID(), "--", splitTwo)
		if len(parts) != splitTwo {
			return fmt.Errorf("malformed component catalog_id %q: expected <type>--<provider>", meta.CatalogID())
		}
		componentType, provider := parts[0], parts[1]
		exists, err := s.compRepo.ExistsByTypeAndProvider(ctx, componentType, provider)
		if err != nil {
			return fmt.Errorf("failed to check running components: %w", err)
		}
		if exists {
			return &validators.ValidationError{
				Code: http.StatusConflict,
				Message: fmt.Sprintf(
					"cannot replace bundle: one or more components are currently running with type %q and provider %q",
					componentType, provider,
				),
			}
		}
	}

	return nil
}

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
