package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func ResetCatalogPassword() error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Validate catalog service and confirm reset action
	shouldProceed, err := validateCatalogServiceAndConfirmReset(deployCtx.Runtime, "password")
	if err != nil {
		return err
	}

	if !shouldProceed {
		return nil
	}

	// Collect new catalog password
	passwordHash, err := promptAndHashPassword()
	if err != nil {
		// Terminate reset password process if failed to collect password

		return err
	}

	logger.InfofCtx(context.Background(), "Deleting catalog secret %s", catalogConstant.CatalogSecretName)
	err = deployCtx.Runtime.DeleteSecret(catalogConstant.CatalogSecretName)
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog secret: %w", err)
	}

	opts, podID, err := catalogUtils.GetCatalogPodConfig(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	logger.InfofCtx(context.Background(), "Deleting existing catalog pod %s", podID)
	err = deployCtx.Runtime.DeletePod(podID, utils.BoolPtr(true))
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	_, err = executeCatalogDeployment(context.Background(), deployCtx, *opts, passwordHash)
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	return nil
}
