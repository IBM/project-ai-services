package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmanruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// JoinAsLocalWorker deploys the worker pod on this machine and connects it to
// the catalog-backend as the "Local" worker.
//
// It uses the already-authenticated catalog client to call POST /api/v1/workers,
// obtaining a real bootstrap token without a second login.
func JoinAsLocalWorker(ctx context.Context, rt *podmanruntime.PodmanClient, opts catalogUtils.PodmanConfigureOptions, c *catalogclient.Client) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	token, gatewayAddr, err := configure.RegisterLocalWorker(ctx, c)
	if err != nil {
		return fmt.Errorf("worker join: %w", err)
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
		return fmt.Errorf("worker join: deploy worker pod: %w", err)
	}

	logger.InfolnCtx(ctx, "worker joined successfully.")

	return nil
}
