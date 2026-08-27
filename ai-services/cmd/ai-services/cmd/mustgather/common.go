package mustgather

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
)

// Compile-time assertions that both gatherers satisfy the podCollector interface.
var _ podCollector = (*podmanGatherer)(nil)
var _ podCollector = (*openshiftGatherer)(nil)

const (
	dirPerm     = 0755
	filePerm    = 0644
	maxLogLines = "1000"
)

// ── file I/O ──────────────────────────────────────────────────────────────────

// writeFile writes content to dir/filename, logging a warning on failure.
func writeFile(ctx context.Context, dir, filename string, content []byte) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, filePerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to write %s: %v\n", path, err)
	}
}

// createOutputDir creates a timestamped must-gather directory inside base and
// returns its path. Hard-fails if the directory cannot be created.
func createOutputDir(base string) (string, error) {
	dir := filepath.Join(base, fmt.Sprintf("must-gather.local.%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	return dir, nil
}

// ── catalog credentials ───────────────────────────────────────────────────────

// collectCatalogCredentials saves ~/.config/ai-services/catalog-credentials.json
// with all token values redacted.
func collectCatalogCredentials(ctx context.Context, san *sanitize.SecretSanitizer, catDir string) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		logger.WarningfCtx(ctx, "Cannot determine user config dir: %v\n", err)

		return
	}

	credsPath := filepath.Join(cfgDir, "ai-services", "catalog-credentials.json")

	data, err := os.ReadFile(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.WarninglnCtx(ctx, "Catalog credentials file not found (not logged in).")
		} else {
			logger.WarningfCtx(ctx, "Failed to read catalog credentials: %v\n", err)
		}

		return
	}

	writeFile(ctx, catDir, "catalog-credentials.json", san.SanitizeJSON(data))
}

// ── catalog installation check ────────────────────────────────────────────────

// checkCatalogInstalled returns true if any pod carrying the
// ai-services.io/application=ai-services label is present in the runtime,
// confirming the catalog has been installed.
func checkCatalogInstalled(ctx context.Context, rt runtime.Runtime) (bool, error) {
	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", catalogConstants.CatalogAppName)},
	})
	if err != nil {
		return false, fmt.Errorf("failed to list catalog pods: %w", err)
	}

	return len(pods) > 0, nil
}

// ── application pod collection ────────────────────────────────────────────────

type podCollector interface {
	collectPod(ctx context.Context, podsDir, podName, namespace string)
}

// collectApplicationPods uses the catalog API to discover pod names for every
// application (or a single named one) and delegates per-pod collection to pc.
func collectApplicationPods(ctx context.Context, pc podCollector, outDir, appName string) []string {
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		logger.WarningfCtx(ctx, "Catalog client unavailable, skipping application pod collection: %v\n", err)

		return nil
	}

	apps, ok := fetchApplicationsForGather(ctx, appClient, appName)
	if !ok {
		return nil
	}

	podsDir := filepath.Join(outDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create pods directory: %v\n", err)

		return nil
	}

	return collectPodsForApps(ctx, pc, appClient, podsDir, apps)
}

// fetchApplicationsForGather fetches the application list from the catalog API
// and normalises warnings. Returns (apps, true) on success, (nil, false) when
// the caller should skip collection.
func fetchApplicationsForGather(ctx context.Context, appClient *catalogClient.ApplicationClient, appName string) ([]catalogTypes.Application, bool) {
	apps, err := cliUtils.FetchApplications(ctx, appClient, appName)
	if err != nil {
		if appName != "" {
			logger.WarningfCtx(ctx, "Application %q not found: %v\n", appName, err)
		} else {
			logger.WarningfCtx(ctx, "Failed to fetch applications: %v\n", err)
		}

		return nil, false
	}

	if len(apps) == 0 {
		if appName != "" {
			logger.WarningfCtx(ctx, "No application named %q found; skipping application pod collection.\n", appName)
		} else {
			logger.WarninglnCtx(ctx, "No applications found; skipping application pod collection.")
		}

		return nil, false
	}

	return apps, true
}

// collectPodsForApps iterates over apps, fetches their PS data from the catalog
// API, and calls pc.collectPod for each pod name returned.
func collectPodsForApps(ctx context.Context, pc podCollector, appClient *catalogClient.ApplicationClient, podsDir string, apps []catalogTypes.Application) []string {
	seen := make(map[string]struct{})
	var namespaces []string

	for _, app := range apps {
		// Derive the app-scoped namespace (OpenShift only; Podman ignores it).
		appNamespace := appNamespaceForID(ctx, app)

		psResp, err := appClient.GetApplicationPS(ctx, app.ID)
		if err != nil {
			logger.WarningfCtx(ctx, "Failed to get PS for application %q: %v\n", app.Name, err)

			continue
		}

		for _, p := range psResp.Services {
			pc.collectPod(ctx, podsDir, p.PodName, appNamespace)
		}

		for _, p := range psResp.Components {
			pc.collectPod(ctx, podsDir, p.PodName, appNamespace)
		}

		// Record the namespace once per app (skip empty — Podman path).
		if appNamespace != "" {
			if _, exists := seen[appNamespace]; !exists {
				seen[appNamespace] = struct{}{}
				namespaces = append(namespaces, appNamespace)
			}
		}
	}

	return namespaces
}

// appNamespaceForID parses app.ID as a UUID and returns the derived OpenShift
// namespace using the same formula as the catalog server. Logs a warning and
// returns an empty string if the ID cannot be parsed.
func appNamespaceForID(ctx context.Context, app catalogTypes.Application) string {
	id, err := uuid.Parse(app.ID)
	if err != nil {
		logger.WarningfCtx(ctx, "Cannot derive namespace for application %q: invalid UUID %q\n", app.Name, app.ID)

		return ""
	}

	return catalogUtils.AppNamespace(id)
}
