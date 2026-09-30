package utils

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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

// WaitForInferenceServiceReady polls the predictor pods of the named KServe InferenceService
// (label "serving.kserve.io/inferenceservice=<isvcName>") until at least one pod exists and
// all of its containers report healthy, mirroring the Podman container readiness wait.
// It returns an error when ctx is done first. Each poll is a short ListPods call, so it
// works the same for local and remote (worker) runtimes.
func WaitForInferenceServiceReady(ctx context.Context, rt runtime.Runtime, isvcName string) error {
	filters := map[string][]string{"label": {"serving.kserve.io/inferenceservice=" + isvcName}}

	for {
		pods, err := rt.ListPods(ctx, filters)
		if err != nil {
			return fmt.Errorf("failed to list pods for InferenceService %q: %w", isvcName, err)
		}

		if predictorPodsReady(pods) {
			logger.InfofCtx(ctx, "InferenceService %q is ready\n", isvcName)

			return nil
		}

		logger.InfofCtx(ctx, "InferenceService %q not ready yet, retrying in %s\n", isvcName, constants.IsvcPollInterval)

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for InferenceService %q to become ready: %w", isvcName, ctx.Err())
		case <-time.After(constants.IsvcPollInterval):
		}
	}
}

// predictorPodsReady reports whether pods is non-empty and every container in every pod is healthy.
func predictorPodsReady(pods []runtimeTypes.Pod) bool {
	if len(pods) == 0 {
		return false
	}

	for _, pod := range pods {
		if len(pod.Containers) == 0 {
			return false
		}

		for _, c := range pod.Containers {
			if c.Health != string(constants.Ready) {
				return false
			}
		}
	}

	return true
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
