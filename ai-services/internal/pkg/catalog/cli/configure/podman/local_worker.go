package podman

import (
	"context"
	"fmt"

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
// A sentinel token (LocalWorkerToken) is passed so the worker pod's
// join guard is satisfied. The catalog-backend gateway skips ValidateToken when
// LOCAL_WORKER=true, so no token needs to live in any TokenStore.
func JoinAsLocalWorker(ctx context.Context, rt *podmanruntime.PodmanClient, opts catalogUtils.PodmanConfigureOptions) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	gatewayAddr := fmt.Sprintf("%s:%d", workerconstants.PodmanGatewayPodName, opts.WorkerGatewayPort)

	workerOpts := workertypes.PodmanWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       workerconstants.LocalWorkerToken,
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
