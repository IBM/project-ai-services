package utils

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/storage/driver"
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
	WorkerGatewayPort int  // gRPC worker gateway port; always active, default 9090
	SkipLocalWorker   bool // When true, skip joining this machine as the Local worker
}

// OpenShiftConfigureOptions contains the configuration for configuring the catalog service on OpenShift runtime.
type OpenShiftConfigureOptions struct {
	Namespace       string
	Timeout         time.Duration
	SkipLocalWorker bool // When true, deploy with localWorker=false
}

// GetCatalogPodConfig retrieves catalog pod configuration by inspecting the running pod and its containers.
// It extracts environment variables like AI_SERVICES_BASE_DIR, DOMAIN_SUFFIX, and CADDY_HTTPS_PORT.
func GetCatalogPodConfig(ctx context.Context, rt runtime.Runtime) (*PodmanConfigureOptions, string, error) {
	label := fmt.Sprintf("%s=%s", catalogConstants.CatalogSecretLabel, catalogConstants.CatalogSecretName)

	podmanOpts, podID, err := cliutils.GetPodConfig(ctx, rt, label)
	if err != nil {
		return nil, "", err
	}

	return &PodmanConfigureOptions{
		BaseDir:           podmanOpts.BaseDir,
		DomainName:        podmanOpts.DomainName,
		HttpsPort:         podmanOpts.HTTPSPort,
		WorkerGatewayPort: podmanOpts.WorkerGatewayPort,
	}, podID, nil
}

// SanitizeFilePath cleans path to prevent path-traversal attacks.
func SanitizeFilePath(path string) string {
	cleanPath := ""
	if path != "" {
		cleanPath = filepath.Clean(path)
	}

	return cleanPath
}

// LoadChartFromFS walks the given filesystem at catalogPath and returns a Helm chart.
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

	if err := helmClient.Uninstall(release, &helm.UninstallOpts{Timeout: uninstallHelmTimeout}); err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			logger.InfofCtx(ctx, "Skipping uninstall of '%s': no release found.", release)

			return nil
		}

		return err
	}

	return nil
}

// Made with Bob
