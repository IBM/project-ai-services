// Package deploy provides worker-node setup and pod deployment helpers.
// It writes prerequisite config files (e.g. Caddyfile), checks whether worker
// components are already running, and deploys pods from the assets/worker
// template tree via EmbedTemplateProvider.
package deploy

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"

	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/openshift"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

func DeployWorker(ctx context.Context, opts workertypes.DeployOpts) error {
	baseDir := opts.BaseDir
	switch types.RuntimeType(opts.RuntimeType) {
	case types.RuntimeTypePodman:
		aiServicesDir, err := utils.ValidateBaseDir(baseDir)
		if err != nil {
			return fmt.Errorf("invalid base directory %q: %w", baseDir, err)
		}

		if err := utils.CreateDir(filepath.Join(aiServicesDir, "models")); err != nil {
			return fmt.Errorf("failed to create model directory: %w", err)
		}

		opts := workertypes.PodmanWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: opts.GatewayAddr,
				Token:       opts.Token,
			},
			Setup: workertypes.Options{
				CommonWorkerOptions: workertypes.CommonWorkerOptions{
					HostAliases: opts.HostAliases,
				},
				BaseDir:     aiServicesDir,
				HTTPSPort:   opts.HTTPSPort,
				DomainName:  opts.DomainName,
				SSLCertPath: opts.SSLCertPath,
				SSLKeyPath:  opts.SSLKeyPath,
			},
		}

		// Setup worker node
		if err := workerpodman.DeployWorker(ctx, opts); err != nil {
			return fmt.Errorf("worker join: setup: %w", err)
		}
	case types.RuntimeTypeOpenShift:
		opts := workertypes.OpenshiftWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: opts.GatewayAddr,
				Token:       opts.Token,
			},
			CommonWorkerOptions: opts.CommonWorkerOptions,
		}
		if err := workeropenshift.DeployWorker(ctx, opts); err != nil {
			return fmt.Errorf("worker join: failed to install worker helm chart: %w", err)
		}
	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.RuntimeType)
	}

	return nil
}
