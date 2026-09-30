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

// registerLocalWorker pre-registers the Local worker using the already-authenticated
// client and returns the bootstrap token.
func registerLocalWorker(ctx context.Context, c *catalogclient.Client) (string, error) {
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

// RegisterLocalWorkerIfNeeded registers the local worker and returns a bootstrap
// token. Registration is skipped and an empty token is returned when BOTH
// conditions hold — signalling to the caller that the worker will reconnect
// without re-registering:
//  1. mtlsSecretExists is true — the TLS encryption key is on disk, so
//     hasValidTLSCredentials will succeed inside the worker container.
//  2. The catalog DB has a completed (non-pending) registration row for "Local"
//     — Restore will find it on the next CommandStream attempt.
func RegisterLocalWorkerIfNeeded(ctx context.Context, mtlsSecretExists bool, c *catalogclient.Client) (string, error) {
	if mtlsSecretExists {
		workers, err := catalogclient.NewWorkerClientFromClient(c).ListWorkers(ctx)
		if err != nil {
			return "", fmt.Errorf("list workers: %w", err)
		}

		if findLocalWorker(workers, true) {
			// Both conditions met — skip registration, reuse existing credentials.
			logger.InfolnCtx(ctx, "Local worker credentials already present — skipping registration.")

			return "", nil
		}
	}

	return registerLocalWorker(ctx, c)
}

// ValidateSkipLocalWorker enforces that --skip-local-worker cannot be changed
// on a re-run when a Local worker is confirmed in the catalog DB.
//
// This function is only reachable when the catalog pod was NOT yet deployed
// (catalogAlreadyDeployed=false) but catalog-secret already exists — meaning a
// previous run created the secrets then failed before pod deployment. When the
// pod is already running, --skip-local-worker is validated earlier against the
// LOCAL_WORKER env var in the running container (validateReconfigureParameters),
// and this function receives isReinstall=false and is a no-op.
//
// Because the pod was never deployed, a successful first run with
// --skip-local-worker=true is impossible here — if it had succeeded the pod
// would be running (catalogAlreadyDeployed=true). The only state that needs
// protecting is a registered Local worker in the DB, which means a previous
// partial run reached worker registration before failing.
//
// When no Local worker exists in the DB there is nothing to protect: allow
// any flag value through so the run can complete cleanly.
//
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

	// No Local worker in the DB — no registered worker to protect.
	// Allow any flag value through; the pod will be deployed fresh.
	if !exists {
		return nil
	}

	// Local worker confirmed in DB: a previous partial run registered it before
	// failing. Block --skip-local-worker=true to avoid orphaning that registration.
	if skipLocalWorker {
		return fmt.Errorf("--skip-local-worker flag cannot be changed on re-run; to change this setting, uninstall and re-run catalog configure")
	}

	return nil
}

// Made with Bob
