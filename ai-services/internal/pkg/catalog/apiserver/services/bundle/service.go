package bundle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate"
	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// CatalogProvider is satisfied by *catalog.CatalogProvider.
// It combines catalog reload and catalog-state query capabilities so both
// concerns are passed as a single dependency. May be nil on CLI / test paths.
type CatalogProvider interface {
	Reload(ctx context.Context) error
	ServiceExists(id string) bool
	ComponentExists(componentType, id string) bool
}

// bundleService implements BundleServiceInterface.
type bundleService struct {
	repo            repository.BundleRepository
	svcRepo         repository.ServiceRepository
	compRepo        repository.ComponentRepository
	catalog         CatalogProvider // nil on CLI / test paths
}

// NewBundleService creates a new bundleService backed by the given repositories.
// catalog may be nil on CLI / test paths — Reload and catalog-check calls are skipped when nil.
func NewBundleService(repo repository.BundleRepository, svcRepo repository.ServiceRepository, compRepo repository.ComponentRepository, catalog CatalogProvider) BundleServiceInterface {
	return &bundleService{
		repo:    repo,
		svcRepo: svcRepo,
		compRepo: compRepo,
		catalog: catalog,
	}
}

// metaFields extracts the four identity strings from either a *bundlemetadata.ServiceMetadata
// or *bundlemetadata.ComponentMetadata. It is called once after peekMetadata so that the
// rest of the pipeline works with plain strings rather than type-switching repeatedly.
func metaFields(meta any) (catalogType, catalogID, version, name string) {
	switch m := meta.(type) {
	case *bundlemetadata.ServiceMetadata:
		return m.Type, m.ID, m.Ver, m.DisplayName
	case *bundlemetadata.ComponentMetadata:
		return m.Type, m.ComponentType + "--" + m.ID, m.Ver, m.DisplayName
	}
	panic("metaFields: unexpected metadata type") // unreachable — parseMetadataYAML rejects all other types
}

// ValidateBundle validates a .tar.gz archive without persisting anything.
//
//  1. Read the archive into memory and parse the root metadata.yaml — all required-field
//     checks (name, description, standalone, component_type) happen here.
//  2. Check for a catalog collision (skipped when catalog is nil).
//  3. Run Podman and OpenShift validators concurrently; each skips gracefully when its
//     runtime directory is absent.
//  4. Return a *ServiceValidationResult or *ComponentValidationResult.
//
// No DB row is written and CatalogProvider is not reloaded.
func (s *bundleService) ValidateBundle(_ context.Context, file io.Reader) (any, error) {
	archiveBytes, meta, err := peekMetadata(file)
	if err != nil {
		return nil, err
	}

	if err := s.checkCatalogCollision(meta); err != nil {
		return nil, err
	}

	// Podman and OpenShift validations are independent — run them concurrently.
	// Each validator resolves topDir lazily from the archive bytes.
	_, _, rootVersion, _ := metaFields(meta)
	runtimeValidators := []validate.BundleValidator{
		validate.NewPodmanBundleValidator(),
		validate.NewOpenShiftBundleValidator(),
	}
	errCh := make(chan error, len(runtimeValidators))
	var wg sync.WaitGroup
	for _, v := range runtimeValidators {
		wg.Add(1)
		go func(validator validate.BundleValidator) {
			defer wg.Done()
			if err := validator.Validate(archiveBytes, "", rootVersion); err != nil {
				errCh <- err
			}
		}(v)
	}
	wg.Wait()
	close(errCh)

	if err := <-errCh; err != nil {
		return nil, err
	}

	return buildValidationResult(meta)
}

// checkCatalogCollision returns a ValidationError when the metadata conflicts with a
// registered catalog entry. Returns nil when catalog is nil (CLI/test paths).
func (s *bundleService) checkCatalogCollision(meta any) error {
	if s.catalog == nil {
		return nil
	}

	switch m := meta.(type) {
	case *bundlemetadata.ServiceMetadata:
		if s.catalog.ServiceExists(m.ID) {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("metadata.yaml: service id %q conflicts with an existing catalog service; choose a unique id", m.ID),
			}
		}
	case *bundlemetadata.ComponentMetadata:
		if s.catalog.ComponentExists(m.ComponentType, m.ID) {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("metadata.yaml: component %q (type: %s) conflicts with an existing catalog component; choose a unique id", m.ID, m.ComponentType),
			}
		}
	}

	return nil
}

// buildValidationResult constructs a *ServiceValidationResult or *ComponentValidationResult
// from the parsed metadata.
func buildValidationResult(meta any) (any, error) {
	switch m := meta.(type) {
	case *bundlemetadata.ServiceMetadata:
		return &ServiceValidationResult{
			Valid:       true,
			CatalogType: m.Type,
			CatalogID:   m.ID,
			Version:     m.Ver,
			Name:        m.DisplayName,
		}, nil
	case *bundlemetadata.ComponentMetadata:
		return &ComponentValidationResult{
			Valid:         true,
			CatalogType:   m.Type,
			ComponentType: m.ComponentType,
			CatalogID:     m.ComponentType + "--" + m.ID,
			Version:       m.Ver,
			Name:          m.DisplayName,
		}, nil
	default:
		return nil, fmt.Errorf("buildValidationResult: unexpected metadata type %T", meta)
	}
}

// ProcessBundle is the synchronous POST creation path.
//
//  1. peekMetadata — read minimal identity fields from the root metadata.yaml.
//  2. Conflict check — query BundleRepository.GetActiveByCatalogID; return
//     *ValidationError{Code:409} if an active row already exists.
//  3. Full archive-based validation — metadata, runtime structure, Helm chart.
//  4. Extract archive to bundleDirPath(catalogType, catalogID, version),
//     stripping the top-level directory.
//  5. Insert DB row via BundleRepository.Insert (status=processing).
//  6. Mark row active via BundleRepository.Update (status=active, size_bytes, name, version).
//     On failure: mark row failed and return error.
//  7. CatalogProvider.Reload() — rebuilds the in-memory catalog so the now-active bundle
//     is immediately visible. On failure: mark row failed and return error.
//  8. Re-fetch via GetBundleByID and return as *BundleResponse.
//     On failure after step 5: mark row failed and store the error message.
func (s *bundleService) ProcessBundle(ctx context.Context, file io.Reader, userID string) (*BundleResponse, error) {
	// Step 1: peek minimal identity fields from root metadata.yaml.
	archiveBytes, meta, err := peekMetadata(file)
	if err != nil {
		return nil, err
	}

	catalogType, catalogID, version, name := metaFields(meta)

	// Step 2: conflict check — return 409 if an active row already exists.
	existing, err := s.repo.GetActiveByCatalogID(ctx, catalogType, catalogID)
	if err != nil {
		return nil, fmt.Errorf("conflict check failed: %w", err)
	}
	if existing != nil {
		return nil, &validators.ValidationError{
			Code: http.StatusConflict,
			Message: fmt.Sprintf(
				"bundle with catalog_id %q already exists (id: %s); use PUT to update",
				catalogID, existing.ID,
			),
		}
	}

	// Step 3: full validation — same rules as POST /validate, no re-read needed.
	if _, err := s.ValidateBundle(ctx, bytes.NewReader(archiveBytes)); err != nil {
		return nil, err
	}

	// Step 4: extract archive to the permanent bundle directory.
	destDir := bundleDirPath(catalogType, catalogID, version)
	sizeBytes, err := extractAndMeasure(archiveBytes, destDir)
	if err != nil {
		_ = os.RemoveAll(destDir) // best-effort cleanup of any partially-extracted files

		return nil, err
	}

	// Steps 5–8: insert, reload, activate, and return.
	return s.insertActivateAndFetch(ctx, catalogType, catalogID, version, name, destDir, sizeBytes, userID)
}

// insertActivateAndFetch performs steps 5–8 of ProcessBundle:
// insert DB row, mark active, reload catalog, and re-fetch the final row.
func (s *bundleService) insertActivateAndFetch(ctx context.Context, catalogType, catalogID, version, name, destDir string, sizeBytes int64, userID string) (*BundleResponse, error) {
	// Step 5: insert DB row with status=processing.
	row := &models.CatalogBundle{
		Name:        name,
		CatalogType: catalogType,
		CatalogID:   catalogID,
		Version:     version,
		CreatedBy:   userID,
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		_ = os.RemoveAll(destDir) // best-effort cleanup of the extracted directory

		return nil, fmt.Errorf("failed to insert bundle record: %w", err)
	}

	// Step 6: mark row active so Reload() can see it as "active" when it queries the DB.
	statusActive := models.BundleStatusActive
	activeName := name
	activeVersion := version
	if updateErr := s.repo.Update(ctx, row.ID, models.BundleUpdate{
		Status:    &statusActive,
		SizeBytes: &sizeBytes,
		Name:      &activeName,
		Version:   &activeVersion,
	}); updateErr != nil {
		s.markFailed(ctx, row.ID, updateErr.Error())

		return nil, fmt.Errorf("failed to activate bundle: %w", updateErr)
	}

	// Step 7: reload the catalog so the now-active bundle is immediately visible.
	if s.catalog != nil {
		if reloadErr := s.catalog.Reload(ctx); reloadErr != nil {
			s.markFailed(ctx, row.ID, reloadErr.Error())

			return nil, fmt.Errorf("catalog reload failed after bundle creation: %w", reloadErr)
		}
	}

	// Step 8: re-fetch the authoritative row from DB and return.
	return s.GetBundleByID(ctx, row.ID.String())
}

// ReplaceBundle is the synchronous PUT update path.
//
//  1. peekMetadata: read minimal identity fields from the archive.
//  2. Immutability check: catalogID and catalogType must match existing record.
//     Returns *ValidationError{Code:422} on mismatch.
//  3. Full archive-based validation — metadata, runtime structure, Helm chart.
//  4. Mark existing row processing via BundleRepository.Update.
//  5. Extract archive to a staging directory (<catalog_id>-<version>-new).
//  6. Rename staging directory into the final path (bundleDirPath).
//  7. UPDATE existing row in-place (status=active, version, name, size_bytes) via BundleRepository.Update.
//  8. CatalogProvider.Reload() — rebuilds the in-memory catalog so the replaced bundle is
//     immediately visible. On failure: mark row failed and return error.
//  9. Delete old on-disk directory when it differs from the new final path.
//  10. Re-fetch via BundleRepository.GetByID and return as *BundleResponse.
//     On failure after step 4: mark row failed, store error message.
func (s *bundleService) ReplaceBundle(ctx context.Context, existing *BundleResponse, file io.Reader, _ string) (*BundleResponse, error) {
	// Step 1: peek minimal identity fields from root metadata.yaml.
	archiveBytes, meta, err := peekMetadata(file)
	if err != nil {
		return nil, err
	}

	catalogType, catalogID, version, name := metaFields(meta)

	// Step 2: immutability check — catalog_id and catalog_type must not change.
	if catalogID != existing.CatalogID {
		return nil, &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"catalog_id mismatch: archive contains %q but existing bundle has %q",
				catalogID, existing.CatalogID,
			),
		}
	}
	if catalogType != existing.CatalogType {
		return nil, &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"catalog_type mismatch: archive contains %q but existing bundle has %q",
				catalogType, existing.CatalogType,
			),
		}
	}

	// Step 3: full validation — same rules as POST /validate, no re-read needed.
	if _, err := s.ValidateBundle(ctx, bytes.NewReader(archiveBytes)); err != nil {
		return nil, err
	}

	// Step 3a: guard — reject if any running service/component is using this catalog entry.
	if err := s.checkNoRunningInstances(ctx, "replace", catalogType, catalogID); err != nil {
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
	resp, replaceErr := s.replaceBundleFiles(ctx, existingID, existing, catalogType, catalogID, version, name, archiveBytes)
	if replaceErr != nil {
		s.markFailed(ctx, existingID, replaceErr.Error())

		return nil, replaceErr
	}

	return resp, nil
}

// replaceBundleFiles performs the file-system and DB operations for ReplaceBundle
// after the row has been moved to "processing". Called only by ReplaceBundle.
func (s *bundleService) replaceBundleFiles(ctx context.Context, existingID uuid.UUID, existing *BundleResponse, catalogType, catalogID, version, name string, archiveBytes []byte) (*BundleResponse, error) {
	oldDir := bundleDirPath(existing.CatalogType, existing.CatalogID, existing.Version)
	newFinalDir := bundleDirPath(catalogType, catalogID, version)
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
	activeName := name
	activeVersion := version
	if updateErr := s.repo.Update(ctx, existingID, models.BundleUpdate{
		Status:    &statusActive,
		SizeBytes: &sizeBytes,
		Name:      &activeName,
		Version:   &activeVersion,
	}); updateErr != nil {
		return nil, fmt.Errorf("failed to activate replaced bundle: %w", updateErr)
	}

	// Step 8: reload the catalog so the replaced bundle is immediately visible.
	if s.catalog != nil {
		if reloadErr := s.catalog.Reload(ctx); reloadErr != nil {
			return nil, fmt.Errorf("catalog reload failed after bundle replacement: %w", reloadErr)
		}
	}

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

// DeleteBundle synchronously marks the row deleting, guards against running
// instances, removes the on-disk directory, reloads CatalogProvider, and
// deletes the DB row.
//
// It accepts the *BundleResponse returned by GetBundleByID — the same shape
// used by ReplaceBundle — so the handler does not need to construct an
// internal BundleRecord manually.
//
//  1. Guard: checkNoRunningInstances — returns 409 Conflict if live workloads
//     still reference this bundle's catalog entry.
//  2. Mark row deleting via BundleRepository.Update.
//  3. Delete on-disk directory: bundleDirPath(existing.CatalogType, existing.CatalogID, existing.Version).
//  4. CatalogProvider.Reload() — rebuilds the in-memory catalog so the deleted bundle
//     is no longer served. On failure: mark row failed and return error.
//  5. Delete DB row via BundleRepository.Delete.
//     On failure before step 5: mark row failed.
func (s *bundleService) DeleteBundle(ctx context.Context, existing *BundleResponse) error {
	// Step 1: guard — reject if any running service/component uses this catalog entry.
	if err := s.checkNoRunningInstances(ctx, "delete", existing.CatalogType, existing.CatalogID); err != nil {
		return err
	}

	existingID, err := uuid.Parse(existing.ID)
	if err != nil {
		return fmt.Errorf("invalid existing bundle id %q: %w", existing.ID, err)
	}

	// Step 2: mark row deleting.
	statusDeleting := models.BundleStatusDeleting
	if updateErr := s.repo.Update(ctx, existingID, models.BundleUpdate{Status: &statusDeleting}); updateErr != nil {
		return fmt.Errorf("failed to mark bundle as deleting: %w", updateErr)
	}

	// Steps 3–5: remove on-disk dir, reload catalog, delete DB row.
	// Any failure before the DB delete marks the row failed.
	if deleteErr := s.deleteBundleFiles(ctx, existingID, existing); deleteErr != nil {
		s.markFailed(ctx, existingID, deleteErr.Error())

		return deleteErr
	}

	return nil
}

// deleteBundleFiles removes the on-disk bundle directory and then permanently
// deletes the DB row. Called only by DeleteBundle after the row is "deleting".
func (s *bundleService) deleteBundleFiles(ctx context.Context, existingID uuid.UUID, existing *BundleResponse) error {
	// Step 3: delete on-disk directory (best-effort — missing dir is not fatal).
	dirPath := bundleDirPath(existing.CatalogType, existing.CatalogID, existing.Version)
	if err := os.RemoveAll(dirPath); err != nil {
		return fmt.Errorf("failed to remove bundle directory %q: %w", dirPath, err)
	}

	// Step 4: reload the catalog so the deleted bundle is no longer served.
	if s.catalog != nil {
		if reloadErr := s.catalog.Reload(ctx); reloadErr != nil {
			return fmt.Errorf("catalog reload failed after bundle deletion: %w", reloadErr)
		}
	}

	// Step 5: delete DB row.
	if err := s.repo.Delete(ctx, existingID); err != nil {
		return fmt.Errorf("failed to delete bundle record: %w", err)
	}

	return nil
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
// identified by catalogType + catalogID.  It is called before ReplaceBundle
// and DeleteBundle to prevent in-flight replacements or deletions while live
// workloads depend on the bundle.
//
//   - For a service bundle:  checks the services table by catalog_id.
//   - For a component bundle: the catalog_id is "<type>--<provider>"; both
//     halves are matched against the components table (type + provider).
func (s *bundleService) checkNoRunningInstances(ctx context.Context, operation, catalogType, catalogID string) error {
	switch catalogType {
	case bundlemetadata.CatalogTypeService:
		exists, err := s.svcRepo.ExistsByCatalogID(ctx, catalogID)
		if err != nil {
			return fmt.Errorf("failed to check running services: %w", err)
		}
		if exists {
			return &validators.ValidationError{
				Code: http.StatusConflict,
				Message: fmt.Sprintf(
					"cannot %s bundle: one or more services are currently running with catalog_id %q",
					operation, catalogID,
				),
			}
		}

	case bundlemetadata.CatalogTypeComponent:
		// catalog_id is "<component_type>--<provider>"; split on the first "--".
		parts := strings.SplitN(catalogID, "--", splitTwo)
		if len(parts) != splitTwo {
			return fmt.Errorf("malformed component catalog_id %q: expected <type>--<provider>", catalogID)
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
					"cannot %s bundle: one or more components are currently running with type %q and provider %q",
					operation, componentType, provider,
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
