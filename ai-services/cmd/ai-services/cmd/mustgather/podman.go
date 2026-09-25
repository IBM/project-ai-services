package mustgather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmanRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workerConstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	modelsSeparatorW = 60 // width of the separator line in models.txt
)

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
func (g *podmanGatherer) gather(ctx context.Context, opts gatherOptions) (string, error) {
	logger.InfolnCtx(ctx, "Starting must-gather for Podman runtime…")

	rt, err := podmanRuntime.NewPodmanClient()
	if err != nil {
		logger.WarningfCtx(ctx, "Could not connect to Podman: %v\n", err)

		return "", fmt.Errorf("failed to connect to Podman: %w", err)
	}

	outDir, err := createOutputDir(opts.outputDir)
	if err != nil {
		return "", err
	}

	logger.InfofCtx(ctx, "Output directory: %s\n", outDir)

	catalogInstalled, err := checkCatalogInstalled(ctx, rt)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to check catalog installation: %v\n", err)
	}

	if catalogInstalled {
		g.collectCatalogInstalled(ctx, rt, outDir, opts.applicationName)
	} else {
		g.collectWorkerOnly(ctx, rt, outDir, opts.applicationName)
	}

	// Always collected — independent of catalog state.
	g.collectSystemInfo(ctx, outDir)
	g.collectNetworkInfo(ctx, outDir)
	g.collectSecretInfo(ctx, outDir)
	g.collectVolumeInfo(ctx, outDir)

	return outDir, nil
}

func (g *podmanGatherer) collectCatalogInstalled(ctx context.Context, rt *podmanRuntime.PodmanClient, outDir, appName string) {
	g.resolveBaseDir(ctx, rt)
	g.collectCatalogArtifacts(ctx, outDir)

	isLocalWorker, err := workercommon.IsPodmanLocalWorker(ctx, rt)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to check local worker: %v\n", err)
	} else if isLocalWorker {
		g.collectWorkerArtifacts(ctx, outDir)
		_ = collectApplicationPods(ctx, g, outDir, appName, workerConstants.LocalWorkerName)
	}

	g.collectModelsInfo(ctx, outDir)
}

func (g *podmanGatherer) collectWorkerOnly(ctx context.Context, rt *podmanRuntime.PodmanClient, outDir, appName string) {
	logger.InfolnCtx(ctx, "No catalog pods found on this node. Collecting worker and application pods...")
	g.collectWorkerArtifacts(ctx, outDir)

	workerName, err := workercommon.ResolveWorkerName(ctx, rt)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to resolve worker name: %v\n", err)

		return
	}

	_ = collectApplicationPods(ctx, g, outDir, appName, workerName)
}

// resolveBaseDir attempts to read AI_SERVICES_BASE_DIR from the running
// catalog backend container env (same approach as `catalog configure --reset-*`).
// Sets g.baseDir to the resolved value, or to the default if the backend pod
// is stopped or the value is empty.
func (g *podmanGatherer) resolveBaseDir(ctx context.Context, rt *podmanRuntime.PodmanClient) {
	g.baseDir = pkgutils.GetBaseDir() // safe fallback

	catalogPodLabel := constants.PodComponentKey + "=" + catalogConstants.CatalogComponentValue
	config, _, err := catalogUtils.GetCatalogPodConfig(ctx, rt, catalogPodLabel)
	if err != nil {
		if errors.Is(err, cliUtils.ErrPodNotFound) {
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

// collectPod collects inspect JSON, container logs, and env vars for one pod.
func (g *podmanGatherer) collectPod(ctx context.Context, podsDir, podName, _ string) {
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

	writeFile(ctx, podDir, "inspect.json", g.sanitizer.SanitizeJSON(raw))
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

	writeFile(ctx, inspectDir, name+".json", g.sanitizer.SanitizeJSON(raw))
}

func (g *podmanGatherer) collectContainerLogs(ctx context.Context, podDir, name string) {
	logsDir := filepath.Join(podDir, "logs")
	if err := os.MkdirAll(logsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create logs directory: %v\n", err)

		return
	}

	raw, err := cliUtils.PodmanRun("logs", "--tail", fmt.Sprintf("%d", maxLogLines), name)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to get logs for container %q: %v\n", name, err)

		return
	}

	writeFile(ctx, logsDir, name+".log", g.sanitizer.SanitizeText(raw))
}

// ── catalog artifact collection ───────────────────────────────────────────────

// collectCatalogArtifacts gathers data for the catalog infrastructure
// (always collected, regardless of --application):
//   - catalog pods (ai-services--catalog, ai-services--db, ai-services--caddy)
//   - catalog-credentials.json with tokens redacted
func (g *podmanGatherer) collectCatalogArtifacts(ctx context.Context, outDir string) {
	logger.InfolnCtx(ctx, "Collecting catalog artifacts…")

	catDir := filepath.Join(outDir, "catalog")
	if err := os.MkdirAll(catDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create catalog directory: %v\n", err)

		return
	}

	g.collectPodsByTemplate(ctx, catDir, catalogConstants.CatalogAppTemplate)
	collectCatalogCredentials(ctx, g.sanitizer, catDir)
}

// collectWorkerArtifacts gathers data for the worker infrastructure (into worker/pods/).
func (g *podmanGatherer) collectWorkerArtifacts(ctx context.Context, outDir string) {
	workerDir := filepath.Join(outDir, "worker")
	g.collectPodsByTemplate(ctx, workerDir, workerConstants.WorkerAppTemplate)
}

// collectPodsByTemplate lists all pods belonging to a given template
// (e.g. template=catalog or template=worker) and delegates to collectPod for each one.
func (g *podmanGatherer) collectPodsByTemplate(ctx context.Context, targetDir, templateName string) {
	raw, err := cliUtils.PodmanRun(
		"pod", "ps",
		"--filter", fmt.Sprintf("label=%s=%s", constants.ApplicationTemplateKey, templateName),
		"--format", "json",
	)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list %s pods: %v\n", templateName, err)

		return
	}

	var pods []map[string]any
	if err := json.Unmarshal(raw, &pods); err != nil {
		logger.WarningfCtx(ctx, "Failed to parse %s pod list: %v\n", templateName, err)

		return
	}

	if len(pods) == 0 {
		return
	}

	podsDir := filepath.Join(targetDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create %s pods directory: %v\n", templateName, err)

		return
	}

	for _, pod := range pods {
		// Pod JSON from `podman pod ps` uses "Name" (string), not "Names" (array).
		name, _ := pod["Name"].(string)
		if name == "" {
			continue
		}

		g.collectPod(ctx, podsDir, name, "") // infrastructure pods have no app-scoped namespace
	}
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

	writeFile(ctx, modelsDir, "models.txt", []byte(strings.Join(lines, "\n")+"\n"))
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

	writeFile(ctx, secDir, "secrets.json", g.sanitizer.SanitizeJSON(raw))
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

		writeFile(ctx, sysDir, c.filename, g.sanitizer.SanitizeText(raw))
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

	writeFile(ctx, netDir, "networks.json", g.sanitizer.SanitizeJSON(raw))
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

	writeFile(ctx, volDir, "volumes.json", g.sanitizer.SanitizeJSON(raw))
}
