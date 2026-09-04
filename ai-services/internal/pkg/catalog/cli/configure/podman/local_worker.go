package podman

import (
	"context"
	"fmt"
	"path/filepath"

	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// JoinAsLocalWorker joins the catalog machine itself as the reserved "Local"
// worker. It can be skipped with --skip-local-worker.
//
// The gateway address uses the host's outbound IP. An empty bootstrap token is passed; the gateway applies the
// local-bypass registration path when it sees an empty token together with
// worker_name == LocalWorkerName.
func JoinAsLocalWorker(ctx context.Context, opts catalogUtils.PodmanConfigureOptions) error {
	logger.InfolnCtx(ctx, "Joining local machine as worker...")

	aiServicesDir, err := utils.ValidateBaseDir(opts.BaseDir)
	if err != nil {
		return fmt.Errorf("local worker join: invalid base directory %q: %w", opts.BaseDir, err)
	}

	if err := utils.CreateDir(filepath.Join(aiServicesDir, "models")); err != nil {
		return fmt.Errorf("local worker join: failed to create model directory: %w", err)
	}

	hostIP, err := utils.GetHostIP()
	if err != nil {
		return fmt.Errorf("local worker join: resolve host IP: %w", err)
	}

	gatewayAddr := fmt.Sprintf("%s:%d", hostIP, opts.WorkerGatewayPort)

	workerOpts := workertypes.PodmanWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       "",
		},
		Setup: workertypes.Options{
			BaseDir:     aiServicesDir,
			HTTPSPort:   opts.HttpsPort,
			DomainName:  opts.DomainName,
			SSLCertPath: catalogUtils.SanitizeFilePath(opts.SSLCertPath),
			SSLKeyPath:  catalogUtils.SanitizeFilePath(opts.SSLKeyPath),
		},
	}

	return workerpodman.DeployWorker(ctx, workerOpts)
}
