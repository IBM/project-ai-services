package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func ResetCatalogPassword() error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Collect new catalog password
	passwordHash, err := promptAndHashPassword()
	if err != nil {
		// Terminate reset password process if failed to collect password

		return err
	}

	logger.Infof("Deleting catalog secret %s", catalogConstant.CatalogSecretName)
	err = deployCtx.Runtime.DeleteSecret(catalogConstant.CatalogSecretName)
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog secret: %w", err)
	}

	opts, err := getAndDeleteCatalogPod(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	_, err = executeCatalogDeployment(context.Background(), deployCtx, *opts, passwordHash)
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	return nil
}

func getAndDeleteCatalogPod(rt runtime.Runtime) (*catalogUtils.PodmanConfigureOptions, error) {
	opts, podID, err := getCatalogPodDetails(rt)
	if err != nil {
		return nil, err
	}

	logger.Infof("Deleting existing catalog pod %s", podID)
	err = rt.DeletePod(podID, utils.BoolPtr(true))
	if err != nil {
		return nil, fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	return opts, nil
}

// getCatalogPodDetails retrieves catalog pod configuration by inspecting the running pod and its containers.
func getCatalogPodDetails(rt runtime.Runtime) (*catalogUtils.PodmanConfigureOptions, string, error) {
	config, podID, err := catalogUtils.GetCatalogPodConfig(rt)
	if err != nil {
		return nil, "", err
	}

	return config, podID, nil
}
