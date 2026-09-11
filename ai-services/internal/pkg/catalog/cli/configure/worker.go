package configure

import (
	"context"
	"fmt"

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

// Made with Bob
