package openshift

import (
	"context"
	"fmt"

	clicommon "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common"
	utils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/utils"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	internalutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	openshiftruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workeruninstall "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

// UninstallCatalog removes the catalog helm release and optionally cleans up PVCs and catalog namespace.
func UninstallCatalog(ctx context.Context, opts utils.UninstallOptions) error {
	catalog := catalogConstants.CatalogAppName
	namespace := catalog

	rt, err := openshiftruntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Check before catalog pods are deleted whether a local worker is co-located.
	isLocalWorker, err := workercommon.IsOpenShiftLocalWorker(ctx, rt)
	if err != nil {
		return fmt.Errorf("failed to check worker: %w", err)
	}

	// Confirm deletion unless auto-yes is set
	if confirmed, err := confirmDeletion(ctx, rt, opts.AutoYes); err != nil || !confirmed {
		return err
	}

	if err := uninstallCatalogResources(ctx, rt, catalog, namespace, opts.SkipCleanup); err != nil {
		return err
	}

	// Only uninstall the co-located worker if LOCAL_WORKER is true
	if isLocalWorker {
		if err := workeruninstall.Uninstall(ctx, workerutils.UninstallOptions{
			RuntimeType: types.RuntimeTypeOpenShift,
			AutoYes:     true,
			SkipCleanup: opts.SkipCleanup,
		}); err != nil {
			return fmt.Errorf("worker uninstall failed: %w", err)
		}
	}

	return nil
}

func uninstallCatalogResources(ctx context.Context, rt runtime.Runtime, catalog, namespace string, skipCleanup bool) error {
	logger.InfolnCtx(ctx, "Proceeding with uninstall...")

	s := spinner.New("Uninstalling catalog service...")
	s.Start(ctx)

	if err := catalogutils.HelmUninstall(ctx, namespace, catalog); err != nil {
		s.Fail("failed to uninstall catalog")

		return fmt.Errorf("failed to uninstall catalog: %w", err)
	}

	if !skipCleanup {
		appLabel := fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, catalog)

		logger.DebuglnCtx(ctx, "Delete catalog PVCs...")

		if err := rt.DeletePVCs(ctx, appLabel); err != nil {
			s.Fail("failed to delete catalog pvc")

			return fmt.Errorf("failed to delete PVCs: %w", err)
		}

		logger.DebuglnCtx(ctx, "Delete catalog secrets...")

		if err := rt.DeleteSecrets(ctx, appLabel); err != nil {
			s.Fail("failed to delete catalog secrets")

			return fmt.Errorf("failed to delete secrets: %w", err)
		}

		if err := rt.DeleteNamespace(ctx, namespace); err != nil {
			s.Fail("failed to delete catalog namespace")

			return fmt.Errorf("failed to delete '%s' namespace: %w", namespace, err)
		}
	}

	s.Stop("Catalog service uninstalled successfully")

	return nil
}

func confirmDeletion(ctx context.Context, rt runtime.Runtime, autoYes bool) (bool, error) {
	pods, err := clicommon.GetCatalogPods(ctx, rt)
	if err != nil || len(pods) == 0 {
		return false, err
	}

	return internalutils.ConfirmUninstall(ctx, pods, autoYes)
}

// Made with Bob
