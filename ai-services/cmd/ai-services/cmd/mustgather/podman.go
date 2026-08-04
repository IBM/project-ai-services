package mustgather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmanRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
)

const (
	dirPerm          = 0755
	filePerm         = 0644
	maxLogLines      = "1000"
	modelsSeparatorW = 60 // width of the separator line in models.txt
)

// gatherOptions carries options forwarded from the cobra command.
type gatherOptions struct {
	outputDir       string
	applicationName string
}

// podmanGatherer collects must-gather data from a Podman runtime via the
// catalog framework (for pod/container discovery) and direct podman CLI
// invocations (for logs, network, volume, and system info).
type podmanGatherer struct {
	sanitizer *sanitize.SecretSanitizer
	baseDir   string // resolved once in gather(); never empty
}

func newPodmanGatherer() *podmanGatherer {
	return &podmanGatherer{sanitizer: sanitize.NewSecretSanitizer()}
}

// ── entry point ───────────────────────────────────────────────────────────────

// gather creates a timestamped output directory and runs every collection step.
// Errors within individual steps are logged as warnings so a partial failure
// never aborts the overall collection.
//
// Collection is split into two tiers:
//   - Catalog-dependent: app pods, catalog artifacts, models — skipped when
//     no catalog pods exist at all (catalog was never installed).
//   - Always-on: system info, secrets, network, volumes — Podman-level data
//     that is useful regardless of catalog state.
func (g *podmanGatherer) gather(opts gatherOptions) (string, error) {
	ctx := context.Background()

	logger.InfolnCtx(ctx, "Starting must-gather for Podman runtime…")

	rt, err := podmanRuntime.NewPodmanClient()
	if err != nil {
		logger.WarningfCtx(ctx, "Could not connect to Podman: %v\n", err)

		return "", fmt.Errorf("failed to connect to Podman: %w", err)
	}

	outDir, err := g.createOutputDir(opts.outputDir)
	if err != nil {
		return "", err
	}

	logger.InfofCtx(ctx, "Output directory: %s\n", outDir)

	catalogInstalled, err := checkCatalogInstalled(rt)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to check catalog installation: %v\n", err)
	}

	if catalogInstalled {
		g.resolveBaseDir(ctx, rt)
	}

	if catalogInstalled {
		g.collectCatalogArtifacts(ctx, outDir)
		g.collectApplicationPods(ctx, outDir, opts.applicationName)
		g.collectModelsInfo(ctx, outDir)
	} else {
		logger.WarninglnCtx(ctx, "No catalog pods found — catalog is not installed. Skipping application pods, catalog artifacts, and models collection.")
	}

	// Always collected — independent of catalog state.
	g.collectSystemInfo(ctx, outDir)
	g.collectNetworkInfo(ctx, outDir)
	g.collectSecretInfo(ctx, outDir)
	g.collectVolumeInfo(ctx, outDir)

	return outDir, nil
}

// checkCatalogInstalled returns true if any pod carrying the
// ai-services.io/application=ai-services label is present, confirming that
// the catalog has been installed (covers catalog, db, and caddy pods — any one
// of them is sufficient).
func checkCatalogInstalled(rt *podmanRuntime.PodmanClient) (bool, error) {
	pods, err := rt.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", catalogConstants.CatalogAppName)},
	})
	if err != nil {
		return false, fmt.Errorf("failed to list catalog pods: %w", err)
	}

	return len(pods) > 0, nil
}

// resolveBaseDir attempts to read AI_SERVICES_BASE_DIR from the running
// catalog backend container env (same approach as `catalog configure --reset-*`).
// Sets g.baseDir to the resolved value, or to the default if the backend pod
// is stopped or the value is empty.
func (g *podmanGatherer) resolveBaseDir(ctx context.Context, rt *podmanRuntime.PodmanClient) {
	g.baseDir = pkgutils.GetBaseDir() // safe fallback

	config, _, err := catalogUtils.GetCatalogPodConfig(rt)
	if err != nil {
		if errors.Is(err, catalogUtils.ErrCatalogPodNotFound) {
			logger.WarninglnCtx(ctx, "Catalog backend pod is stopped — base directory resolved to default.")
		} else {
			logger.WarningfCtx(ctx, "Could not read base dir from catalog pod: %v; using default.\n", err)
		}

		return
	}

	if config.BaseDir != "" {
		g.baseDir = config.BaseDir
	}

	logger.InfofCtx(ctx, "Using base directory: %s\n", g.baseDir)
}

func (g *podmanGatherer) createOutputDir(base string) (string, error) {
	dir := filepath.Join(base, fmt.Sprintf("must-gather.local.%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	return dir, nil
}

// ── application pod collection ────────────────────────────────────────────────

// collectApplicationPods uses the catalog API to discover pod names for every
// application (or a single named one), then collects inspect/logs/env for each.
func (g *podmanGatherer) collectApplicationPods(ctx context.Context, outDir, appName string) {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		logger.WarningfCtx(ctx, "Catalog client unavailable, skipping application pod collection: %v\n", err)

		return
	}

	apps, ok := fetchApplicationsForGather(ctx, appClient, appName)
	if !ok {
		return
	}

	podsDir := filepath.Join(outDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create pods directory: %v\n", err)

		return
	}

	g.collectPodsForApps(ctx, appClient, podsDir, apps)
}

// fetchApplicationsForGather fetches the application list and logs the
// appropriate warning on error or empty result. Returns (apps, true) on
// success, (nil, false) when the caller should skip collection.
func fetchApplicationsForGather(ctx context.Context, appClient *catalogClient.ApplicationClient, appName string) ([]catalogTypes.Application, bool) {
	apps, err := cliUtils.FetchApplications(appClient, appName)
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

// collectPodsForApps iterates over apps, fetches their PS data, and collects
// each pod. Extracted to keep collectApplicationPods within complexity limits.
func (g *podmanGatherer) collectPodsForApps(ctx context.Context, appClient *catalogClient.ApplicationClient, podsDir string, apps []catalogTypes.Application) {
	for _, app := range apps {
		psResp, err := appClient.GetApplicationPS(app.ID)
		if err != nil {
			logger.WarningfCtx(ctx, "Failed to get PS for application %q: %v\n", app.Name, err)

			continue
		}

		for _, p := range psResp.Services {
			g.collectPod(ctx, podsDir, p.PodName)
		}

		for _, p := range psResp.Components {
			g.collectPod(ctx, podsDir, p.PodName)
		}
	}
}

// collectPod collects inspect JSON, container logs, and env vars for one pod.
func (g *podmanGatherer) collectPod(ctx context.Context, podsDir, podName string) {
	podDir := filepath.Join(podsDir, podName)
	if err := os.MkdirAll(podDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create directory for pod %q: %v\n", podName, err)

		return
	}

	g.collectPodInspect(ctx, podDir, podName)
	g.collectContainersForPod(ctx, podDir, podName)
}

func (g *podmanGatherer) collectPodInspect(ctx context.Context, podDir, podName string) {
	raw, err := cliUtils.PodmanRun("pod", "inspect", podName)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to inspect pod %q: %v\n", podName, err)

		return
	}

	g.writeFile(ctx, podDir, "inspect.json", g.sanitizer.SanitizeJSON(raw))
}

// collectContainersForPod lists every non-infra container in podName and
// collects its logs and environment variables.
func (g *podmanGatherer) collectContainersForPod(ctx context.Context, podDir, podName string) {
	raw, err := cliUtils.PodmanRun("ps", "-a", "--filter", "pod="+podName, "--format", "json")
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list containers for pod %q: %v\n", podName, err)

		return
	}

	var containers []map[string]any
	if err := json.Unmarshal(raw, &containers); err != nil {
		logger.WarningfCtx(ctx, "Failed to parse container list for pod %q: %v\n", podName, err)

		return
	}

	for _, c := range containers {
		name := cliUtils.PodmanContainerName(c)
		if name == "" || strings.HasSuffix(name, "-infra") {
			continue // skip infra/pause containers — no useful data
		}

		g.collectContainerInspect(ctx, podDir, name)
		g.collectContainerLogs(ctx, podDir, name)
	}
}

// collectContainerInspect runs `podman inspect <name>` and writes the full
// sanitized JSON. This covers Config.Env, Mounts, NetworkSettings, State,
// Image, Labels — making a separate env-vars extraction step unnecessary.
func (g *podmanGatherer) collectContainerInspect(ctx context.Context, podDir, name string) {
	raw, err := cliUtils.PodmanRun("inspect", name)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to inspect container %q: %v\n", name, err)

		return
	}

	inspectDir := filepath.Join(podDir, "inspect")
	if err := os.MkdirAll(inspectDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create inspect directory: %v\n", err)

		return
	}

	g.writeFile(ctx, inspectDir, name+".json", g.sanitizer.SanitizeJSON(raw))
}

func (g *podmanGatherer) collectContainerLogs(ctx context.Context, podDir, name string) {
	logsDir := filepath.Join(podDir, "logs")
	if err := os.MkdirAll(logsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create logs directory: %v\n", err)

		return
	}

	raw, err := cliUtils.PodmanRun("logs", "--tail", maxLogLines, name)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to get logs for container %q: %v\n", name, err)

		return
	}

	g.writeFile(ctx, logsDir, name+".log", g.sanitizer.SanitizeText(raw))
}

// ── catalog artifact collection ───────────────────────────────────────────────

// collectCatalogArtifacts gathers data for the catalog infrastructure
// (always collected, regardless of --application):
//   - catalog pods (ai-services--catalog, ai-services--db, ai-services--caddy)
//   - Caddyfile from <BaseDir>/common/caddy/ (reverse-proxy route config)
//   - catalog-credentials.json with tokens redacted
func (g *podmanGatherer) collectCatalogArtifacts(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting catalog artifacts…")

	catDir := filepath.Join(outDir, "catalog")
	if err := os.MkdirAll(catDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create catalog directory: %v\n", err)

		return
	}

	g.collectCatalogPods(ctx, catDir)
	g.collectCaddyfile(ctx, catDir)
	g.collectCatalogCredentials(ctx, catDir)
}

// collectCatalogPods lists all pods labelled ai-services.io/application=ai-services
// and delegates to collectPod for each one.
func (g *podmanGatherer) collectCatalogPods(ctx context.Context, catDir string) {
	raw, err := cliUtils.PodmanRun(
		"pod", "ps",
		"--filter", "label=ai-services.io/application=ai-services",
		"--format", "json",
	)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list catalog pods: %v\n", err)

		return
	}

	var pods []map[string]any
	if err := json.Unmarshal(raw, &pods); err != nil {
		logger.WarningfCtx(ctx, "Failed to parse catalog pod list: %v\n", err)

		return
	}

	if len(pods) == 0 {
		logger.WarninglnCtx(ctx, "No catalog pods found (catalog may not be configured).")

		return
	}

	podsDir := filepath.Join(catDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create catalog pods directory: %v\n", err)

		return
	}

	for _, pod := range pods {
		// Pod JSON from `podman pod ps` uses "Name" (string), not "Names" (array).
		name, _ := pod["Name"].(string)
		if name == "" {
			continue
		}

		g.collectPod(ctx, podsDir, name)
	}
}

// collectCaddyfile copies:
//   - <BaseDir>/common/caddy/Caddyfile        — static reverse-proxy config
//   - <BaseDir>/common/caddy-config/caddy/autosave.json — Caddy's live config snapshot
func (g *podmanGatherer) collectCaddyfile(ctx context.Context, catDir string) {
	caddyFiles := []struct {
		src      string
		dst      string
		sanitize func([]byte) []byte
	}{
		{
			src:      filepath.Join(g.baseDir, "common", "caddy", "Caddyfile"),
			dst:      "Caddyfile",
			sanitize: g.sanitizer.SanitizeText,
		},
		{
			src:      filepath.Join(g.baseDir, "common", "caddy-config", "caddy", "autosave.json"),
			dst:      "caddy-autosave.json",
			sanitize: g.sanitizer.SanitizeJSON,
		},
	}

	for _, f := range caddyFiles {
		data, err := os.ReadFile(f.src)
		if err != nil {
			if os.IsNotExist(err) {
				logger.WarningfCtx(ctx, "%s not found (catalog may not be configured)\n", f.src)
			} else {
				logger.WarningfCtx(ctx, "Failed to read %s: %v\n", f.src, err)
			}

			continue
		}

		g.writeFile(ctx, catDir, f.dst, f.sanitize(data))
	}
}

// collectCatalogCredentials saves the CLI credentials file
// (~/.config/ai-services/catalog-credentials.json) with tokens redacted.
func (g *podmanGatherer) collectCatalogCredentials(ctx context.Context, catDir string) {
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

	g.writeFile(ctx, catDir, "catalog-credentials.json", g.sanitizer.SanitizeJSON(data))
}

// ── models info collection ────────────────────────────────────────────────────

// collectModelsInfo records which models are present under <BaseDir>/models/
// and how much disk space each one occupies. Model weights are never copied —
// only the directory listing and per-model disk usage are written.
func (g *podmanGatherer) collectModelsInfo(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting models information…")

	modelsPath := filepath.Join(g.baseDir, "models")

	entries, err := os.ReadDir(modelsPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.WarningfCtx(ctx, "Models directory not found at %s\n", modelsPath)
		} else {
			logger.WarningfCtx(ctx, "Failed to read models directory: %v\n", err)
		}

		return
	}

	modelsDir := filepath.Join(outDir, "models")
	if err := os.MkdirAll(modelsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create models output directory: %v\n", err)

		return
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Models directory: %s", modelsPath))
	lines = append(lines, strings.Repeat("-", modelsSeparatorW))

	for _, org := range entries {
		if !org.IsDir() {
			continue
		}

		// Each top-level dir is an org (e.g. ibm-granite); subdirs are model names.
		orgPath := filepath.Join(modelsPath, org.Name())
		modelEntries, err := os.ReadDir(orgPath)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %s/  (unreadable: %v)", org.Name(), err))

			continue
		}

		for _, model := range modelEntries {
			if !model.IsDir() {
				continue
			}

			modelPath := filepath.Join(orgPath, model.Name())
			size, fileCount := pkgutils.DirStats(modelPath)
			lines = append(lines, fmt.Sprintf(
				"  %s/%s  (%s, %d files)",
				org.Name(), model.Name(), pkgutils.FormatBytes(size), fileCount,
			))
		}
	}

	g.writeFile(ctx, modelsDir, "models.txt", []byte(strings.Join(lines, "\n")+"\n"))
}

// ── secret metadata collection ────────────────────────────────────────────────

// collectSecretInfo lists all Podman secrets and writes their metadata
// (ID, name, driver, created/updated timestamps). Secret values are never
// exposed — `podman secret ls` never outputs stored secret data.
func (g *podmanGatherer) collectSecretInfo(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting secret metadata…")

	secDir := filepath.Join(outDir, "secrets")
	if err := os.MkdirAll(secDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create secrets directory: %v\n", err)

		return
	}

	raw, err := cliUtils.PodmanRun("secret", "ls", "--format", "json")
	if err != nil {
		logger.WarningfCtx(ctx, "podman secret ls failed: %v\n", err)

		return
	}

	g.writeFile(ctx, secDir, "secrets.json", g.sanitizer.SanitizeJSON(raw))
}

// ── system / network / volume collection ──────────────────────────────────────

func (g *podmanGatherer) collectSystemInfo(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting system information…")

	sysDir := filepath.Join(outDir, "system")
	if err := os.MkdirAll(sysDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create system directory: %v\n", err)

		return
	}

	cmds := []struct {
		filename string
		args     []string
	}{
		{"version.txt", []string{"version"}},
		{"info.json", []string{"info", "--format", "json"}},
		{"system-df.txt", []string{"system", "df"}},
	}

	for _, c := range cmds {
		raw, err := cliUtils.PodmanRun(c.args...)
		if err != nil {
			logger.WarningfCtx(ctx, "podman %s failed: %v\n", strings.Join(c.args, " "), err)

			continue
		}

		g.writeFile(ctx, sysDir, c.filename, g.sanitizer.SanitizeText(raw))
	}
}

func (g *podmanGatherer) collectNetworkInfo(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting network information…")

	netDir := filepath.Join(outDir, "network")
	if err := os.MkdirAll(netDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create network directory: %v\n", err)

		return
	}

	raw, err := cliUtils.PodmanRun("network", "ls", "--format", "json")
	if err != nil {
		logger.WarningfCtx(ctx, "podman network ls failed: %v\n", err)

		return
	}

	g.writeFile(ctx, netDir, "networks.json", g.sanitizer.SanitizeJSON(raw))
}

func (g *podmanGatherer) collectVolumeInfo(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting volume information…")

	volDir := filepath.Join(outDir, "volumes")
	if err := os.MkdirAll(volDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create volumes directory: %v\n", err)

		return
	}

	raw, err := cliUtils.PodmanRun("volume", "ls", "--format", "json")
	if err != nil {
		logger.WarningfCtx(ctx, "podman volume ls failed: %v\n", err)

		return
	}

	g.writeFile(ctx, volDir, "volumes.json", g.sanitizer.SanitizeJSON(raw))
}

// ── file I/O ──────────────────────────────────────────────────────────────────

func (g *podmanGatherer) writeFile(ctx context.Context, dir, filename string, content []byte) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, filePerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to write %s: %v\n", path, err)
	}
}
