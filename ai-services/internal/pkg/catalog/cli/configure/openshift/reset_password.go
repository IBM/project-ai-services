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

	// Update catalog admin password in secret
	err = rt.UpdateSecret(catalogConstants.CatalogSecretName, passwordSecretData)
	if err != nil {
		return fmt.Errorf("failed to reset catalog password: %w", err)
	}

	err = rt.RolloutRestartDeployment(catalogConstants.CatalogDeploymentName)
	if err != nil {
		return fmt.Errorf("failed to restart catalog deployment: %w", err)
	}

	return nil
}
