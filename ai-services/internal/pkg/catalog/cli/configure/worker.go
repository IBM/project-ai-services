package configure

import (
	"context"
	"fmt"
	"strings"

	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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
// client and returns the bootstrap token and gateway address.
func RegisterLocalWorker(ctx context.Context, c *catalogclient.Client) (token, gatewayAddr string, err error) {
	logger.InfolnCtx(ctx, "Registering worker via catalog API...")

	resp, err := catalogclient.NewWorkerClientFromClient(c).CreateWorker(ctx, workerconstants.LocalWorkerName)
	if err != nil {
		return "", "", fmt.Errorf("register worker: %w", err)
	}

	return resp.Token, resp.GatewayAddress, nil
}

// CheckLocalWorkerSkip determines whether local-worker registration can be
// skipped because valid on-disk TLS credentials already exist.
//
// It returns skip=true when BOTH conditions hold:
//  1. The worker-mtls-encryption-secret is present — the TLS encryption key is
//     on disk, so hasValidTLSCredentials will succeed inside the worker container.
//  2. The catalog DB has a completed (non-pending) registration row for "Local"
//     — Restore will find it on the next CommandStream attempt.
//
// secretExists is rt.SecretExists from either PodmanClient or OpenshiftClient;
// passing the method directly avoids the need for a new interface.
func CheckLocalWorkerSkip(ctx context.Context, secretExists func(context.Context, string) (bool, error), c *catalogclient.Client) (skip bool, err error) {
	exists, err := secretExists(ctx, workerconstants.WorkerMTLSSecretName)
	if err != nil {
		return false, fmt.Errorf("check worker mTLS secret: %w", err)
	}

	if !exists {
		return false, nil
	}

	workers, err := catalogclient.NewWorkerClientFromClient(c).ListWorkers(ctx)
	if err != nil {
		return false, fmt.Errorf("list workers: %w", err)
	}

	for _, w := range workers {
		if strings.EqualFold(w.Name, workerconstants.LocalWorkerName) && w.Status != "pending" {
			return true, nil
		}
	}

	return false, nil
}

// ValidateSkipLocalWorker enforces that --skip-local-worker cannot be
// changed on a re-run. It infers what the original run used from the catalog DB:
//
//   - workers registered (len > 0)  → original run had --skip-local-worker=false
//   - no workers registered (len == 0) → original run had --skip-local-worker=true
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

	workersExist := len(workers) > 0

	// workersExist == true  → original flag was false (local worker was joined)
	// workersExist == false → original flag was true  (local worker was skipped)
	if skipLocalWorker == workersExist {
		return fmt.Errorf("--skip-local-worker flag cannot be changed on re-run; to change this setting, uninstall and re-run catalog configure")
	}

	return nil
}

// Made with Bob
