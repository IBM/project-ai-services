package utils

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/assets"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
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

// OpenShiftConfigureOptions contains the configuration for configuring the catalog service on OpenShift runtime.
type OpenShiftConfigureOptions struct {
	Namespace string
	Timeout   time.Duration
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

// LoadChartFromCatalogFS walks assets.CatalogFS at catalogPath and returns a Helm chart.
func LoadChartFromCatalogFS(catalogPath string) (helmchart.Charter, error) {
	var files []*archive.BufferedFile

	// Determine the values.yaml path for this chart's runtime
	runtimeStr := string(vars.RuntimeFactory.GetRuntimeType())
	valuesPath := filepath.Join(catalogPath, runtimeStr, "values.yaml")

	// Read and process @generate annotations in values.yaml once, before walking (only if the file exists)
	var processedValuesData []byte
	if _, err := fs.Stat(&assets.CatalogFS, valuesPath); err == nil {
		valuesData, err := assets.CatalogFS.ReadFile(valuesPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read values.yaml at %s: %w", valuesPath, err)
		}
		processedValuesData, err = utils.ProcessGenerateAnnotationsFromYAML(valuesData)
		if err != nil {
			return nil, fmt.Errorf("failed to process generate annotations: %w", err)
		}
	}

	err := fs.WalkDir(&assets.CatalogFS, catalogPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := assets.CatalogFS.ReadFile(p)
		if err != nil {
			return err
		}

		// Use processed values.yaml data (with generated passwords) instead of the raw file
		if filepath.ToSlash(p) == filepath.ToSlash(valuesPath) {
			data = processedValuesData
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

// Made with Bob
