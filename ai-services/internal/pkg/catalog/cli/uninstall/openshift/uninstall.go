package openshift

import (
	"context"
	"fmt"
	"time"

	clicommon "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common"
	utils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/utils"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	openshiftruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
)

const defaultUninstallTimeout = 5 * time.Minute

// UninstallCatalog removes the catalog helm release and optionally cleans up PVCs.
func UninstallCatalog(ctx context.Context, opts utils.UninstallOptions) error {
	catalog := catalogConstants.CatalogAppName
	namespace := catalog

	// Create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	// Check if the catalog release exists
	if installed, err := isCatalogInstalled(ctx, helmClient, catalog, namespace); err != nil || !installed {
		return err
	}

	rt, err := openshiftruntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Confirm deletion unless auto-yes is set
	if confirmed, err := confirmDeletion(ctx, rt, opts.AutoYes); err != nil || !confirmed {
		return err
	}

	logger.InfolnCtx(ctx, "Proceeding with uninstall...")

	s := spinner.New("Uninstalling catalog '" + catalog + "'...")
	s.Start(ctx)

	if err := helmClient.Uninstall(catalog, &helm.UninstallOpts{Timeout: defaultUninstallTimeout}); err != nil {
		s.Fail("failed to uninstall catalog")

		return fmt.Errorf("failed to uninstall catalog: %w", err)
	}

	s.Stop("Catalog '" + catalog + "' uninstalled successfully")

	return cleanupPVCs(ctx, rt, opts.SkipCleanup, catalog)
}

func confirmDeletion(ctx context.Context, rt runtime.Runtime, autoYes bool) (bool, error) {
	if autoYes {
		return true, nil
	}

	pods, err := clicommon.GetCatalogPods(ctx, rt)
	if err != nil || len(pods) == 0 {
		return false, err
	}

	return utils.ConfirmDeletion(ctx, pods)
}

func isCatalogInstalled(ctx context.Context, helmClient *helm.Helm, catalog, namespace string) (bool, error) {
	exists, err := helmClient.IsReleaseExist(catalog)
	if err != nil {
		return false, fmt.Errorf("failed to check if catalog exists: %w", err)
	}

	if !exists {
		logger.InfofCtx(ctx, "Catalog '%s' does not exist in namespace '%s'\n", catalog, namespace)

		return false, nil
	}

	return true, nil
}

func cleanupPVCs(ctx context.Context, rt runtime.Runtime, skipCleanup bool, catalog string) error {
	if skipCleanup {
		return nil
	}

	logger.DebuglnCtx(ctx, "Cleaning up Persistent Volume Claims...")

	if err := rt.DeletePVCs(fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, catalog)); err != nil {
		return fmt.Errorf("failed to cleanup PVCs: %w", err)
	}

	return nil
}

// Made with Bob
