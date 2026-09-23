package configure

import (
	"context"
	"fmt"
	"strings"

	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// LoginToCatalog logs in to the catalog API and returns the authenticated client.
// A successful return both proves the credentials are correct and provides a
// client that callers can reuse — no second login is needed.
func LoginToCatalog(ctx context.Context, catalogAPIURL, adminPassword string) (*catalogclient.Client, error) {
	logger.InfolnCtx(ctx, "Logging in to catalog API...")

	c, err := catalogclient.NewWithLogin(ctx, catalogAPIURL, catalogconstants.CatalogAdminUser, adminPassword, true)
	if err != nil {
		return nil, fmt.Errorf("login to catalog API at %s: %w", catalogAPIURL, err)
	}

	logger.InfolnCtx(ctx, "Admin credentials verified.")

	return c, nil
}

// RegisterLocalWorker pre-registers the Local worker using the already-authenticated
// client and returns the bootstrap token.
func RegisterLocalWorker(ctx context.Context, c *catalogclient.Client) (string, error) {
	logger.InfolnCtx(ctx, "Registering worker via catalog API...")

	resp, err := catalogclient.NewWorkerClientFromClient(c).CreateWorker(ctx, workerconstants.LocalWorkerName)
	if err != nil {
		return "", fmt.Errorf("register worker: %w", err)
	}

	return resp.Token, nil
}

// findLocalWorker reports whether a worker named "Local" appears in the
// provided list. When requireReady is true, only a non-pending entry matches.
func findLocalWorker(workers []catalogtypes.Worker, requireReady bool) bool {
	for _, w := range workers {
		if strings.EqualFold(w.Name, workerconstants.LocalWorkerName) {
			if !requireReady || w.Status != "pending" {
				return true
			}
		}
	}

	return false
}

// CheckLocalWorkerSkip determines whether local-worker registration can be
// skipped because valid on-disk TLS credentials already exist.
//
// It returns skip=true when BOTH conditions hold:
//  1. mtlsSecretExists is true — the TLS encryption key is on disk, so
//     hasValidTLSCredentials will succeed inside the worker container.
//  2. The catalog DB has a completed (non-pending) registration row for "Local"
//     — Restore will find it on the next CommandStream attempt.
func CheckLocalWorkerSkip(ctx context.Context, mtlsSecretExists bool, c *catalogclient.Client) (skip bool, err error) {
	if !mtlsSecretExists {
		return false, nil
	}

	workers, err := catalogclient.NewWorkerClientFromClient(c).ListWorkers(ctx)
	if err != nil {
		return false, fmt.Errorf("list workers: %w", err)
	}

	return findLocalWorker(workers, true), nil
}

// RegisterLocalWorkerIfNeeded registers the local worker and returns a bootstrap
// token. If valid on-disk credentials and a completed catalog registration already
// exist, registration is skipped and an empty token is returned — signalling to
// the caller that the worker will reconnect without re-registering.
func RegisterLocalWorkerIfNeeded(ctx context.Context, mtlsSecretExists bool, c *catalogclient.Client) (string, error) {
	skip, err := CheckLocalWorkerSkip(ctx, mtlsSecretExists, c)
	if err != nil {
		return "", err
	}

	if skip {
		// Both conditions met — skip registration, reuse existing credentials.
		logger.InfolnCtx(ctx, "Local worker credentials already present — skipping registration.")

		return "", nil
	}

	return RegisterLocalWorker(ctx, c)
}

// ValidateSkipLocalWorker enforces that --skip-local-worker cannot be
// changed on a re-run. It infers what the original run used from the catalog DB:
//
//   - local worker registered → original run had --skip-local-worker=false
//   - local worker absent     → original run had --skip-local-worker=true
//
// An error is returned when the current flag value contradicts that state.
// This function is a no-op on a fresh install (isReinstall=false).
func ValidateSkipLocalWorker(ctx context.Context, c *catalogclient.Client, isReinstall bool, skipLocalWorker bool) error {
	if !isReinstall {
		return nil
	}

	workers, err := catalogclient.NewWorkerClientFromClient(c).ListWorkers(ctx)
	if err != nil {
		return fmt.Errorf("skip-local-worker validation: list workers: %w", err)
	}

	// Only check for the Local worker — remote workers may exist in the DB
	// from a previous run and must not influence this decision.
	exists := findLocalWorker(workers, false)

	// exists == true  → original flag was false (local worker was joined)
	// exists == false → original flag was true  (local worker was skipped)
	if skipLocalWorker == exists {
		return fmt.Errorf("--skip-local-worker flag cannot be changed on re-run; to change this setting, uninstall and re-run catalog configure")
	}

	return nil
}

// Made with Bob
