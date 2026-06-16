package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func ResetPodmanAuth() error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Verify auth file path exists before deleting secret
	_, err = utils.GetAuthFilePath()
	if err != nil {
		return err
	}

	// Delete podman auth secret.
	logger.Infof("Deleting catalog podman auth secret %s", catalogConstant.CatalogPodmanAuthSecretName)
	err = deployCtx.Runtime.DeleteSecret(catalogConstant.CatalogPodmanAuthSecretName)
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog podman auth secret: %w", err)
	}

	opts, err := getAndDeleteCatalogPod(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	_, err = executeCatalogDeployment(context.Background(), deployCtx, *opts, "")
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	return nil
}
