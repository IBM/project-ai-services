package podman

import (
	"context"
	"fmt"
	"strconv"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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

func getAndDeleteCatalogPod(rt runtime.Runtime) (*PodmanConfigureOptions, error) {
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
func getCatalogPodDetails(rt runtime.Runtime) (*PodmanConfigureOptions, string, error) {
	// Build filter to find all pods using the catalog secret via label
	logger.Infof("Getting catalog pod details")
	filter := map[string][]string{
		"label": {fmt.Sprintf(
			"%s=%s",
			catalogConstant.CatalogSecretLabel,
			catalogConstant.CatalogSecretName,
		)},
	}

	// List all pods that reference the catalog secret
	pods, err := rt.ListPods(filter)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, "", fmt.Errorf("no catalog pod found")
	}

	// Inspect catalog pod
	pod := pods[0]
	pInfo, err := rt.InspectPod(pod.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	opts := &PodmanConfigureOptions{}

	for _, container := range pInfo.Containers {
		// Inspect container for get hold of envs
		cInfo, err := rt.InspectContainer(container.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
		}
		getEnvValues(cInfo.Env, opts)
	}

	return opts, pod.ID, nil
}

func getEnvValues(podEnv map[string]string, opts *PodmanConfigureOptions) {
	// Setting required 3 envs
	if value, ok := podEnv["AI_SERVICES_BASE_DIR"]; ok {
		opts.BaseDir = value
	}
	if value, ok := podEnv["DOMAIN_SUFFIX"]; ok {
		opts.DomainName = value
	}
	if value, ok := podEnv["CADDY_HTTPS_PORT"]; ok {
		opts.HttpsPort, _ = strconv.Atoi(value)
	}
}
