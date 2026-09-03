package utils

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/assets"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

const uninstallHelmTimeout = 5 * time.Minute

var (
	ErrCatalogPodNotFound = fmt.Errorf("no catalog pod found")
)

// PodmanConfigureOptions contains the configuration for configuring the catalog service on Podman runtime.
type PodmanConfigureOptions struct {
	BaseDir           string
	DomainName        string // Custom domain name for self-signed certificates
	SSLCertPath       string // Path to user-provided SSL certificate
	SSLKeyPath        string // Path to user-provided SSL private key
	HttpsPort         int
	WorkerGatewayPort int // gRPC worker gateway port; always active, default 9090
}

// OpenShiftConfigureOptions contains the configuration for configuring the catalog service on OpenShift runtime.
type OpenShiftConfigureOptions struct {
	Namespace string
	Timeout   time.Duration
}

// GetCatalogPodConfig retrieves catalog pod configuration by inspecting the running pod and its containers.
// It extracts environment variables like AI_SERVICES_BASE_DIR, DOMAIN_SUFFIX, and CADDY_HTTPS_PORT.
func GetCatalogPodConfig(ctx context.Context, rt runtime.Runtime) (*PodmanConfigureOptions, string, error) {
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
	pods, err := rt.ListPods(ctx, filter)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, "", ErrCatalogPodNotFound
	}

	// Inspect catalog pod
	pod := pods[0]
	pInfo, err := rt.InspectPod(ctx, pod.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	config := &PodmanConfigureOptions{}

	for _, container := range pInfo.Containers {
		// Inspect container to get environment variables
		cInfo, err := rt.InspectContainer(ctx, container.ID)
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
	if value, ok := podEnv["WORKER_GATEWAY_PORT"]; ok {
		config.WorkerGatewayPort, _ = strconv.Atoi(value)
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

// LoadChartFromCatalogFS walks assets.CatalogFS at catalogPath and returns a Helm chart.
func LoadChartFromCatalogFS(catalogPath string) (helmchart.Charter, error) {
	return LoadChartFromFS(&assets.CatalogFS, catalogPath)
}

// LoadChartFromFS walks the given filesystem at catalogPath and returns a Helm chart.
// Use this for bundle items that are stored on disk (os.DirFS) rather than embedded.
func LoadChartFromFS(fsys fs.FS, catalogPath string) (helmchart.Charter, error) {
	var files []*archive.BufferedFile

	err := fs.WalkDir(fsys, catalogPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}

		rel := strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(catalogPath)+"/")
		files = append(files, &archive.BufferedFile{Name: rel, Data: data})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk chart directory %s: %w", catalogPath, err)
	}

	return loader.LoadFiles(files)
}

func HelmUninstall(ctx context.Context, namespace, release string) error {
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	exists, err := helmClient.IsReleaseExist(release)
	if err != nil {
		return fmt.Errorf("failed to check '%s' release existence: %w", release, err)
	}

	if !exists {
		logger.InfofCtx(ctx, "Skipping uninstall of '%s': no release found.", release)

		return nil
	}

	return helmClient.Uninstall(release, &helm.UninstallOpts{Timeout: uninstallHelmTimeout})
}

// Made with Bob
