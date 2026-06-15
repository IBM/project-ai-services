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

const (
	catalogContainerName = "ai-services--catalog-backend"
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

	podEnv, err := getAndDeleteCatalogPod(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	baseDir, domainName, httpsPort := getFlagValues(podEnv)
	opts := PodmanConfigureOptions{
		BaseDir:    baseDir,
		DomainName: domainName,
		HttpsPort:  httpsPort,
	}

	_, err = executeCatalogDeployment(context.Background(), deployCtx, opts, passwordHash)
	if err != nil {
		return fmt.Errorf("failed to deploy catalog pod: %w", err)
	}

	return nil
}

func getAndDeleteCatalogPod(rt runtime.Runtime) (map[string]string, error) {
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
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no catalog pod found")
	}

	// Inspect catalog pod
	pod := pods[0]
	podID := ""
	podEnv := map[string]string{}
	pInfo, err := rt.InspectPod(pod.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	for _, container := range pInfo.Containers {
		if container.Name == catalogContainerName {
			// Inspect container for get hold of envs
			cInfo, err := rt.InspectContainer(container.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
			}
			podID = pod.ID
			podEnv = cInfo.Env

			break
		}
	}

	logger.Infof("Deleting existing catalog pod %s", podID)
	err = rt.DeletePod(podID, utils.BoolPtr(true))
	if err != nil {
		return podEnv, fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	return podEnv, nil
}

func getFlagValues(podEnv map[string]string) (string, string, int) {
	// Setting baseDir global variable
	var baseDir, domainName string
	var httpsPort int
	if value, ok := podEnv["AI_SERVICES_BASE_DIR"]; ok {
		baseDir = value
	}

	// Setting domainName global variable
	if value, ok := podEnv["DOMAIN_SUFFIX"]; ok {
		domainName = value
	}

	// Setting httpsPort global variable
	if value, ok := podEnv["CADDY_HTTPS_PORT"]; ok {
		httpsPort, _ = strconv.Atoi(value)
	}

	return baseDir, domainName, httpsPort
}
