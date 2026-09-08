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
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
)

// UninstallCatalog removes the catalog helm release and optionally cleans up PVCs and catalog namespace.
func UninstallCatalog(ctx context.Context, opts utils.UninstallOptions) error {
	catalog := catalogConstants.CatalogAppName
	namespace := catalog

	rt, err := openshiftruntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Confirm deletion unless auto-yes is set
	if confirmed, err := confirmDeletion(ctx, rt, opts.AutoYes); err != nil || !confirmed {
		return err
	}

	logger.InfolnCtx(ctx, "Proceeding with uninstall...")

	s := spinner.New("Uninstalling catalog service...")
	s.Start(ctx)

	if err := catalogutils.HelmUninstall(ctx, namespace, catalog); err != nil {
		s.Fail("failed to uninstall catalog")

		return fmt.Errorf("failed to uninstall catalog: %w", err)
	}

	if !opts.SkipCleanup {
		logger.DebuglnCtx(ctx, "Delete catalog PVCs...")

		if err := rt.DeletePVCs(ctx, fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, catalog)); err != nil {
			s.Fail("failed to delete catalog pvc")

			return fmt.Errorf("failed to delete PVCs: %w", err)
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
