package openshift

import (
	"context"
	"fmt"

	clicommon "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/utils"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	openshiftruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
)

func ResetCatalogPassword() error {
	catalog := catalogConstants.CatalogAppName
	namespace := catalog
	ctx := context.Background()

	// Create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	// Check if the catalog release exists
	if installed, err := clicommon.IsCatalogInstalled(ctx, helmClient, catalog, namespace); err != nil || !installed {
		return err
	}

	// Confirm to start password reset process
	if confirmed, err := cliutils.ConfirmCatalogReset("password"); err != nil || !confirmed {
		return err
	}

	rt, err := openshiftruntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Collect new catalog password
	passwordHash, err := catalogUtils.PromptAndHashPassword()
	if err != nil {
		// Terminate reset password process if failed to collect password

		return err
	}

	passwordSecretData := map[string][]byte{
		"admin-password": []byte(passwordHash),
	}

	s := spinner.New("Reset catalog admin password...")
	s.Start(ctx)

	if err := applyPasswordReset(ctx, rt, passwordSecretData); err != nil {
		s.Fail("Password reset failed.")

		return err
	}

	s.Stop("Password reset completed.")

	return nil
}

func applyPasswordReset(ctx context.Context, rt *openshiftruntime.OpenshiftClient, passwordSecretData map[string][]byte) error {
	// Update catalog admin password in secret
	if err := rt.UpdateSecret(catalogConstants.CatalogSecretName, passwordSecretData); err != nil {
		return fmt.Errorf("failed to reset catalog password: %w", err)
	}

	if err := rt.RolloutRestartDeployment(catalogConstants.CatalogDeploymentName); err != nil {
		return fmt.Errorf("failed to restart catalog deployment: %w", err)
	}

	if err := cliutils.WaitForDeploymentReady(ctx, rt, catalogConstants.CatalogDeploymentName); err != nil {
		return fmt.Errorf("catalog deployment did not become ready: %w", err)
	}

	return nil
}
