package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeOpenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/openshift"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

const (
	// catalogAPIRouteName is the OpenShift route name for the catalog backend API.
	catalogAPIRouteName = "catalog-api"
)

// JoinAsLocalWorker deploys the worker on OpenShift and connects it to the
// catalog-backend as the "Local" worker.
//
// It uses the already-authenticated catalog client to call POST /api/v1/workers,
// obtaining a real bootstrap token without a second login.
func JoinAsLocalWorker(ctx context.Context, rt *runtimeOpenshift.OpenshiftClient, c *catalogclient.Client) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	token, gatewayAddr, err := configure.RegisterLocalWorker(ctx, c)
	if err != nil {
		return fmt.Errorf("local worker join: %w", err)
	}

	opts := workertypes.OpenshiftWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       token,
		},
	}

	if err := workeropenshift.DeployWorker(ctx, opts); err != nil {
		return fmt.Errorf("local worker join: deploy worker: %w", err)
	}

	logger.InfolnCtx(ctx, "Local worker joined successfully.")

	return nil
}

// getCatalogAPIURL looks up the catalog-api OpenShift route and returns the
// full HTTPS URL, e.g. "https://catalog-api.apps.cluster.example.com".
func getCatalogAPIURL(ctx context.Context, rt *runtimeOpenshift.OpenshiftClient) (string, error) {
	routes, err := rt.ListRoutes(ctx, "")
	if err != nil {
		return "", fmt.Errorf("list routes: %w", err)
	}

	for _, r := range routes {
		if r.Name == catalogAPIRouteName {
			return "https://" + r.HostPort, nil
		}
	}

	return "", fmt.Errorf("route %q not found in namespace", catalogAPIRouteName)
}
