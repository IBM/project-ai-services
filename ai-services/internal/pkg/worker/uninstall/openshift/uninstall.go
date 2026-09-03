package openshift

import (
	"context"
	"errors"
	"fmt"

	clituils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// Uninstall removes all worker components deployed by `worker join`.
func Uninstall(ctx context.Context, opts workerutils.UninstallOptions) error {
	namespace := workerconstants.WorkerAppName
	release := workerconstants.WorkerHelmReleaseName

	rt, err := runtime.CreateRuntime(opts.RuntimeType, namespace)
	if err != nil {
		return fmt.Errorf("worker uninstall: init runtime: %w", err)
	}

	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {workerconstants.WorkerPodLabel},
	})
	if err != nil {
		return fmt.Errorf("worker uninstall: list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfolnCtx(ctx, "No worker pods found — nothing to uninstall.")

		return nil
	}

	// Confirm Uninstall unless auto-yes is set
	if confirmed, err := clituils.ConfirmUninstall(ctx, pods, opts.AutoYes); err != nil || !confirmed {
		return err
	}

	logger.InfolnCtx(ctx, "Proceeding with uninstall...")

	s := spinner.New("Uninstalling worker service...")
	s.Start(ctx)

	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	if err := helmClient.UninstallRelease(release); err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			logger.InfofCtx(ctx, "Skipping uninstall of '%s': no release found.", release)

			return nil
		}

		return err
	}

	s.Stop("Worker service uninstalled successfully")

	return nil
}
