package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmanruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// JoinAsLocalWorker deploys the worker pod on this machine and connects it to
// the catalog-backend as the "Local" worker.
//
// It uses the already-authenticated catalog client to call POST /api/v1/workers,
// obtaining a real bootstrap token without a second login.
//
// When the worker's mTLS secret already exists (preserved by --skip-cleanup) AND
// the catalog DB has a completed registration row for "Local" (status ready or
// disconnected), registration is skipped — the worker has valid on-disk TLS
// credentials and will reconnect using them. The pod is still redeployed (it was
// removed during uninstall) with an empty token; StartGrpcStream detects the
// existing credentials and bypasses the Register RPC automatically.
func JoinAsLocalWorker(ctx context.Context, rt *podmanruntime.PodmanClient, opts catalogUtils.PodmanConfigureOptions, c *catalogclient.Client) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	token, err := configure.RegisterLocalWorkerIfNeeded(ctx, rt.SecretExists, c)
	if err != nil {
		return fmt.Errorf("worker join: %w", err)
	}

	// For the co-located local worker on Podman, use the internal pod DNS name
	// directly. This bypasses external DNS resolution and requires no /etc/hosts entries.
	// The internal pod name (PodmanGatewayPodName) is already included in the gateway's TLS SANs.
	gatewayAddr := fmt.Sprintf("%s:%d", workerconstants.PodmanGatewayPodName, opts.WorkerGatewayPort)

	workerOpts := workertypes.PodmanWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       token,
		},
		Setup: workertypes.Options{
			BaseDir:     opts.BaseDir,
			HTTPSPort:   opts.HttpsPort,
			DomainName:  opts.DomainName,
			SSLCertPath: opts.SSLCertPath,
			SSLKeyPath:  opts.SSLKeyPath,
		},
	}

	if err := workerpodman.DeployWorker(ctx, workerOpts); err != nil {
		return fmt.Errorf("worker join: deploy worker pod: %w", err)
	}

	logger.InfolnCtx(ctx, "worker joined successfully.")

	return nil
}
