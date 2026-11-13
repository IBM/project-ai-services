// Package openshift holds the OpenShift deployment helpers shared by the application and model
// flows: building the Helm manager for a runtime, installing catalog charts, installing the
// per-namespace prerequisites and waiting for KServe predictors. It is the OpenShift counterpart
// of the podman utils package.
package openshift

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/project-ai-services/ai-services/assets"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	helmutil "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
)

// prerequisitesRoot is the catalog directory with the charts every namespace needs before any
// component chart (e.g. the KServe ServingRuntimes).
const prerequisitesRoot = "prerequisites/openshift"

// NewHelmManager returns the HelmManager that drives Helm in namespace through rt: over the
// worker's gRPC stream for a remote runtime, against the local kubeconfig otherwise.
func NewHelmManager(rt runtime.Runtime, namespace string) helmutil.HelmManager {
	if rrt, ok := rt.(*remoteruntime.RemoteRuntime); ok {
		return helmutil.NewRemoteHelmManager(rrt.Sender, namespace)
	}

	return helmutil.NewLocalHelmManager(namespace)
}

// InstallChart loads the chart at catalogPath from fsys and installs or upgrades it as release.
// templateID is injected as a --set override so that the ai-services.io/template labels carry the
// database UUID, matching the Podman convention.
func InstallChart(
	ctx context.Context,
	helm helmutil.HelmManager,
	release, catalogPath string,
	fsys fs.FS,
	values map[string]any,
	templateID string,
) error {
	chart, err := catalogutils.LoadChartFromFS(fsys, catalogPath)
	if err != nil {
		return fmt.Errorf("failed to load chart at %s: %w", catalogPath, err)
	}

	logger.InfofCtx(ctx, "Deploying release '%s'\n", release)

	return helm.InstallOrUpgrade(ctx, release, chart, values, templateID, constants.HelmTimeout)
}

// DeployPrerequisites installs every chart under prerequisites/openshift/ into the namespace of
// helm, each subdirectory as its own release named after the directory. It is idempotent.
func DeployPrerequisites(ctx context.Context, helm helmutil.HelmManager) error {
	entries, err := assets.CatalogFS.ReadDir(prerequisitesRoot)
	if err != nil {
		// No prerequisites directory: nothing to install.
		logger.InfofCtx(ctx, "No prerequisites found at %s, skipping\n", prerequisitesRoot)

		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		release := entry.Name() // e.g. "serving-runtime-cpu"

		if err := InstallChart(ctx, helm, release, prerequisitesRoot+"/"+release, &assets.CatalogFS, map[string]any{}, ""); err != nil {
			return fmt.Errorf("failed to deploy prerequisite '%s': %w", release, err)
		}
	}

	return nil
}
