package utils

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// DeployFunc is the work function for a single item: receives its ID and returns an error.
type DeployFunc func(ctx context.Context, id string) error

// StatusFunc updates the DB status for a single item on success or failure.
// The caller closes over the repo and the concrete status value.
type StatusFunc func(ctx context.Context, dbID uuid.UUID, msg string) error

// RunConcurrently fans out deployFn across items in parallel, keyed by ID -> database UUID.
// For each item:
//   - on failure: calls onError(ctx, dbID, errMsg), sends to errCh
//   - on success: calls onSuccess(ctx, dbID, "Deployed successfully")
//
// All errors are collected and returned together.
func RunConcurrently(
	ctx context.Context,
	items map[string]uuid.UUID,
	deployFn DeployFunc,
	onError StatusFunc,
	onSuccess StatusFunc,
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(items))

	for id, dbID := range items {
		wg.Add(1)

		go func(itemID string, databaseID uuid.UUID) {
			defer wg.Done()

			logger.InfofCtx(ctx, "Deploying %s\n", itemID)

			if err := deployFn(ctx, itemID); err != nil {
				// Context cancelled — deletion is in charge of status, exit silently.
				if ctx.Err() != nil {
					logger.InfofCtx(ctx, "Deployment of %s cancelled (deletion in progress)\n", itemID)

					return
				}

				errMsg := fmt.Sprintf("Deployment failed: %v", err)
				if updateErr := onError(ctx, databaseID, errMsg); updateErr != nil {
					logger.ErrorfCtx(ctx, "Failed to update error status for %s: %v\n", itemID, updateErr)
				}
				errCh <- fmt.Errorf("failed to deploy %s: %w", itemID, err)

				return
			}

			if updateErr := onSuccess(ctx, databaseID, "Deployed successfully"); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update running status for %s: %v\n", itemID, updateErr)
			}
		}(id, dbID)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("deployment errors: %v", errs)
	}

	return nil
}

// ChartHasInferenceService reports whether any template file under catalogPath
// in fsys declares a KServe InferenceService (i.e. contains "kind: InferenceService").
// It is used to decide whether WaitForInferenceServiceReady must be called after
// a Helm install — Helm marks an InferenceService "Current" as soon as the CRD is
// accepted, but KServe only sets Ready=True once the predictor pod is fully up.
func ChartHasInferenceService(fsys fs.FS, catalogPath string) bool {
	found := false

	_ = fs.WalkDir(fsys, catalogPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return err
		}

		data, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return nil
		}

		if bytes.Contains(data, []byte("kind: InferenceService")) {
			found = true
		}

		return nil
	})

	return found
}
