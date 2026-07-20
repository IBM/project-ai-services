package openshift

import (
	"context"
	"fmt"
	"time"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	openshiftruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const defaultUninstallTimeout = 5 * time.Minute

// UninstallCatalog removes the catalog helm release and optionally cleans up PVCs.
func UninstallCatalog(ctx context.Context, opts catalogUtils.UninstallOptions) error {
	catalog := catalogConstants.CatalogAppName
	namespace := catalog

	// Create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	// Check if the catalog release exists
	if installed, err := isCatalogInstalled(helmClient, catalog, namespace); err != nil || !installed {
		return err
	}

	// Confirm deletion unless auto-yes is set
	if !opts.AutoYes {
		if confirmed, err := confirmDeletion(catalog); !confirmed || err != nil {
			return err
		}
	}

	logger.Infoln("Proceeding with uninstall...")

	s := spinner.New("Uninstalling catalog '" + catalog + "'...")
	s.Start(ctx)

	if err := helmClient.Uninstall(catalog, &helm.UninstallOpts{Timeout: defaultUninstallTimeout}); err != nil {
		s.Fail("failed to uninstall catalog")

		return fmt.Errorf("failed to uninstall catalog: %w", err)
	}

	s.Stop("Catalog '" + catalog + "' uninstalled successfully")

	return cleanupPVCs(opts.SkipCleanup, catalog, namespace)
}

func isCatalogInstalled(helmClient *helm.Helm, catalog, namespace string) (bool, error) {
	exists, err := helmClient.IsReleaseExist(catalog)
	if err != nil {
		return false, fmt.Errorf("failed to check if catalog exists: %w", err)
	}

	if !exists {
		logger.Infof("Catalog '%s' does not exist in namespace '%s'\n", catalog, namespace)

		return false, nil
	}

	return true, nil
}

func confirmDeletion(catalog string) (bool, error) {
	confirmed, err := utils.ConfirmAction("Are you sure you want to uninstall the catalog '" + catalog + "'?")
	if err != nil {
		return false, fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmed {
		logger.Infoln("Uninstall cancelled")

		return false, nil
	}

	return true, nil
}

func cleanupPVCs(skipCleanup bool, catalog, namespace string) error {
	if skipCleanup {
		return nil
	}

	logger.Debugln("Cleaning up Persistent Volume Claims...")

	rt, err := openshiftruntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	if err := rt.DeletePVCs(fmt.Sprintf("ai-services.io/application=%s", catalog)); err != nil {
		return fmt.Errorf("failed to cleanup PVCs: %w", err)
	}

	return nil
}

// Made with Bob
