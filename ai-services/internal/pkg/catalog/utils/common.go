package utils

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

var (
	ErrCatalogPodNotFound = fmt.Errorf("no catalog pod found")
)

// PodmanConfigureOptions contains the configuration for configuring the catalog service on Podman runtime.
type PodmanConfigureOptions struct {
	BaseDir     string
	DomainName  string // Custom domain name for self-signed certificates
	SSLCertPath string // Path to user-provided SSL certificate
	SSLKeyPath  string // Path to user-provided SSL private key
	HttpsPort   int
}

// UninstallOptions contains the configuration for uninstalling the catalog service.
type UninstallOptions struct {
	Runtime     types.RuntimeType
	AutoYes     bool
	SkipCleanup bool
}

// GetCatalogPodConfig retrieves catalog pod configuration by inspecting the running pod and its containers.
// It extracts environment variables like AI_SERVICES_BASE_DIR, DOMAIN_SUFFIX, and CADDY_HTTPS_PORT.
func GetCatalogPodConfig(rt runtime.Runtime) (*PodmanConfigureOptions, string, error) {
	// Build filter to find all pods using the catalog secret via label
	logger.Debugf("Getting catalog pod configuration")
	filter := map[string][]string{
		"label": {fmt.Sprintf(
			"%s=%s",
			catalogConstants.CatalogSecretLabel,
			catalogConstants.CatalogSecretName,
		)},
	}

	// List all pods that reference the catalog secret
	pods, err := rt.ListPods(filter)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, "", ErrCatalogPodNotFound
	}

	// Inspect catalog pod
	pod := pods[0]
	pInfo, err := rt.InspectPod(pod.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	config := &PodmanConfigureOptions{}

	for _, container := range pInfo.Containers {
		// Inspect container to get environment variables
		cInfo, err := rt.InspectContainer(container.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
		}
		extractConfigFromEnv(cInfo.Env, config)
	}

	return config, pod.ID, nil
}

// extractConfigFromEnv extracts configuration values from container environment variables.
func extractConfigFromEnv(podEnv map[string]string, config *PodmanConfigureOptions) {
	if value, ok := podEnv["AI_SERVICES_BASE_DIR"]; ok {
		config.BaseDir = value
	}
	if value, ok := podEnv["DOMAIN_SUFFIX"]; ok {
		config.DomainName = value
	}
	if value, ok := podEnv["CADDY_HTTPS_PORT"]; ok {
		config.HttpsPort, _ = strconv.Atoi(value)
	}
}

// SanitizeFilePath cleans path to prevent path-traversal attacks.
func SanitizeFilePath(path string) string {
	cleanPath := ""
	if path != "" {
		cleanPath = filepath.Clean(path)
	}

	return cleanPath
}

// ConfirmDeletion prompts the user to confirm deletion and logs pods to be deleted.
func ConfirmDeletion(ctx context.Context, pods []types.Pod) (bool, error) {
	// Print pods to be deleted
	logger.InfofCtx(ctx, "Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	// Confirm deletion
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfolnCtx(ctx, "Deletion cancelled")

		return false, nil
	}

	return true, nil
}

// GetCatalogPods return the list of catalog pod.
func GetCatalogPods(rt runtime.Runtime) ([]types.Pod, error) {
	// Check if catalog pods exist
	pods, err := rt.ListPods(map[string][]string{
		"label": {fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey ,catalogConstants.CatalogAppName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infoln("Catalog service is not deployed")

		return nil, nil
	}

	logger.Infof("Found %d catalog pod(s)\n", len(pods))

	return pods, nil
}

// Made with Bob
