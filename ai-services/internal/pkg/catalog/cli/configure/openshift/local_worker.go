package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeOpenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
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
//
// When the worker's mTLS secret already exists (preserved by --skip-cleanup via
// helm.sh/resource-policy: keep) AND the catalog DB has a completed registration
// row for "Local" (status ready or disconnected), registration is skipped — the
// worker has valid on-disk TLS credentials and will reconnect using them. The
// Helm release is still upgraded (it was removed during uninstall) with an empty
// token; StartGrpcStream detects the existing credentials and bypasses the
// Register RPC automatically.
func JoinAsLocalWorker(ctx context.Context, rt *runtimeOpenshift.OpenshiftClient, c *catalogclient.Client) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	mtlsSecretExists, err := rt.SecretExists(ctx, workerconstants.WorkerMTLSSecretName)
	if err != nil {
		return fmt.Errorf("worker join: check mTLS secret: %w", err)
	}

	token, err := configure.RegisterLocalWorkerIfNeeded(ctx, mtlsSecretExists, c)
	if err != nil {
		return fmt.Errorf("worker join: %w", err)
	}

	// For the co-located local worker on OpenShift, connect directly via the
	// internal service DNS endpoint. This avoids routing out through the OpenShift
	// router/external route. The internal service DNS name is already included in
	// the gateway's TLS server certificate SANs.
	gatewayAddr := fmt.Sprintf("%s:%d", workerconstants.OpenShiftGatewayServiceEndpoint, workerconstants.WorkerGatewayPort)

	opts := workertypes.OpenshiftWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       token,
		},
	}

	if err := workeropenshift.DeployWorker(ctx, opts); err != nil {
		return fmt.Errorf("worker join: deploy worker: %w", err)
	}

	logger.InfolnCtx(ctx, "worker joined successfully.")

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
