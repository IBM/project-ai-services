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
// If the worker pod and its secrets are already present (e.g. preserved by a
// previous --skip-cleanup uninstall), registration and deploy are skipped — the
// running worker will reconnect to the new control plane using its existing
// mTLS credentials.
func JoinAsLocalWorker(ctx context.Context, rt *podmanruntime.PodmanClient, opts catalogUtils.PodmanConfigureOptions, c *catalogclient.Client) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	// If the worker's mTLS secret is already present (preserved by --skip-cleanup),
	// the worker has valid on-disk TLS credentials and can reconnect using them —
	// skip registration (no new token needed) but still redeploy the pod.
	var token, gatewayAddr string

	workerSecretsExist, err := rt.SecretExists(ctx, workerconstants.WorkerMTLSSecretName)
	if err != nil {
		return fmt.Errorf("local worker join: check worker secrets: %w", err)
	}

	if !workerSecretsExist {
		token, gatewayAddr, err = configure.RegisterLocalWorker(ctx, c)
		if err != nil {
			return fmt.Errorf("local worker join: %w", err)
		}
	} else {
		logger.InfolnCtx(ctx, "Local worker credentials already present — skipping registration.")
	}

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
		return fmt.Errorf("local worker join: deploy worker pod: %w", err)
	}

	logger.InfolnCtx(ctx, "Local worker joined successfully.")

	return nil
}
