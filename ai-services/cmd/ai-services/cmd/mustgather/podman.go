package mustgather

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
)

const (
	dirPerm     = 0755
	filePerm    = 0644
	maxLogLines = "1000"
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
}

func newPodmanGatherer() *podmanGatherer {
	return &podmanGatherer{sanitizer: sanitize.NewSecretSanitizer()}
}

// ── entry point ───────────────────────────────────────────────────────────────

// gather creates a timestamped output directory and runs every collection step.
// Errors within individual steps are logged as warnings so a partial failure
// never aborts the overall collection.
func (g *podmanGatherer) gather(opts gatherOptions) (string, error) {
	logger.Infoln("Starting must-gather for Podman runtime…")

	outDir, err := g.createOutputDir(opts.outputDir)
	if err != nil {
		return "", err
	}

	logger.Infof("Output directory: %s\n", outDir)

	g.collectApplicationPods(outDir, opts.applicationName)
	g.collectCatalogArtifacts(outDir)
	g.collectModelsInfo(outDir)
	g.collectSecretInfo(outDir)
	g.collectSystemInfo(outDir)
	g.collectNetworkInfo(outDir)
	g.collectVolumeInfo(outDir)

	return outDir, nil
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
func (g *podmanGatherer) collectApplicationPods(outDir, appName string) {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		logger.Warningf("Catalog client unavailable, skipping application pod collection: %v\n", err)
		return
	}

	apps, err := cliUtils.FetchApplications(appClient, appName)
	if err != nil {
		logger.Warningf("Failed to fetch applications: %v\n", err)
		return
	}

	if len(apps) == 0 {
		logger.Warningln("No applications found; skipping application pod collection.")
		return
	}

	podsDir := filepath.Join(outDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.Warningf("Failed to create pods directory: %v\n", err)
		return
	}

	for _, app := range apps {
		psResp, err := appClient.GetApplicationPS(app.ID)
		if err != nil {
			logger.Warningf("Failed to get PS for application %q: %v\n", app.Name, err)
			continue
		}

		for _, p := range psResp.Services {
			g.collectPod(podsDir, p.PodName)
		}

		for _, p := range psResp.Components {
			g.collectPod(podsDir, p.PodName)
		}
	}
}

// collectPod collects inspect JSON, container logs, and env vars for one pod.
func (g *podmanGatherer) collectPod(podsDir, podName string) {
	podDir := filepath.Join(podsDir, podName)
	if err := os.MkdirAll(podDir, dirPerm); err != nil {
		logger.Warningf("Failed to create directory for pod %q: %v\n", podName, err)
		return
	}

	g.collectPodInspect(podDir, podName)
	g.collectContainersForPod(podDir, podName)
}

func (g *podmanGatherer) collectPodInspect(podDir, podName string) {
	raw, err := podmanRun("pod", "inspect", podName)
	if err != nil {
		logger.Warningf("Failed to inspect pod %q: %v\n", podName, err)
		return
	}

	g.writeFile(podDir, "inspect.json", g.sanitizeJSON(raw))
}

// collectContainersForPod lists every non-infra container in podName and
// collects its logs and environment variables.
func (g *podmanGatherer) collectContainersForPod(podDir, podName string) {
	raw, err := podmanRun("ps", "-a", "--filter", "pod="+podName, "--format", "json")
	if err != nil {
		logger.Warningf("Failed to list containers for pod %q: %v\n", podName, err)
		return
	}

	var containers []map[string]any
	if err := json.Unmarshal(raw, &containers); err != nil {
		logger.Warningf("Failed to parse container list for pod %q: %v\n", podName, err)
		return
	}

	for _, c := range containers {
		name := podmanContainerName(c)
		if name == "" || strings.HasSuffix(name, "-infra") {
			continue // skip infra/pause containers — no useful data
		}

		g.collectContainerInspect(podDir, name)
		g.collectContainerLogs(podDir, name)
	}
}

// collectContainerInspect runs `podman inspect <name>` and writes the full
// sanitized JSON. This covers Config.Env, Mounts, NetworkSettings, State,
// Image, Labels — making a separate env-vars extraction step unnecessary.
func (g *podmanGatherer) collectContainerInspect(podDir, name string) {
	raw, err := podmanRun("inspect", name)
	if err != nil {
		logger.Warningf("Failed to inspect container %q: %v\n", name, err)
		return
	}

	inspectDir := filepath.Join(podDir, "inspect")
	if err := os.MkdirAll(inspectDir, dirPerm); err != nil {
		logger.Warningf("Failed to create inspect directory: %v\n", err)
		return
	}

	g.writeFile(inspectDir, name+".json", g.sanitizeJSON(raw))
}

func (g *podmanGatherer) collectContainerLogs(podDir, name string) {
	logsDir := filepath.Join(podDir, "logs")
	if err := os.MkdirAll(logsDir, dirPerm); err != nil {
		logger.Warningf("Failed to create logs directory: %v\n", err)
		return
	}

	raw, err := podmanRun("logs", "--tail", maxLogLines, name)
	if err != nil {
		logger.Warningf("Failed to get logs for container %q: %v\n", name, err)
		return
	}

	g.writeFile(logsDir, name+".log", g.sanitizeText(raw))
}

// ── catalog artifact collection ───────────────────────────────────────────────

// collectCatalogArtifacts gathers data for the catalog infrastructure
// (always collected, regardless of --application):
//   - catalog pods (ai-services--catalog, ai-services--db, ai-services--caddy)
//   - Caddyfile from <BaseDir>/common/caddy/ (reverse-proxy route config)
//   - catalog-credentials.json with tokens redacted
func (g *podmanGatherer) collectCatalogArtifacts(outDir string) {
	logger.Infoln("Collecting catalog artifacts…")

	catDir := filepath.Join(outDir, "catalog")
	if err := os.MkdirAll(catDir, dirPerm); err != nil {
		logger.Warningf("Failed to create catalog directory: %v\n", err)
		return
	}

	g.collectCatalogPods(catDir)
	g.collectCaddyfile(catDir)
	g.collectCatalogCredentials(catDir)
}

// collectCatalogPods lists all pods labelled ai-services.io/application=ai-services
// and delegates to collectPod for each one.
func (g *podmanGatherer) collectCatalogPods(catDir string) {
	raw, err := podmanRun(
		"pod", "ps",
		"--filter", "label=ai-services.io/application=ai-services",
		"--format", "json",
	)
	if err != nil {
		logger.Warningf("Failed to list catalog pods: %v\n", err)
		return
	}

	var pods []map[string]any
	if err := json.Unmarshal(raw, &pods); err != nil {
		logger.Warningf("Failed to parse catalog pod list: %v\n", err)
		return
	}

	if len(pods) == 0 {
		logger.Warningln("No catalog pods found (catalog may not be configured).")
		return
	}

	podsDir := filepath.Join(catDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.Warningf("Failed to create catalog pods directory: %v\n", err)
		return
	}

	for _, pod := range pods {
		// Pod JSON from `podman pod ps` uses "Name" (string), not "Names" (array).
		name, _ := pod["Name"].(string)
		if name == "" {
			continue
		}

		g.collectPod(podsDir, name)
	}
}

// collectCaddyfile copies:
//   - <BaseDir>/common/caddy/Caddyfile        — static reverse-proxy config
//   - <BaseDir>/common/caddy-config/caddy/autosave.json — Caddy's live config snapshot
func (g *podmanGatherer) collectCaddyfile(catDir string) {
	baseDir := pkgutils.GetBaseDir()

	caddyFiles := []struct {
		src      string
		dst      string
		sanitize func([]byte) []byte
	}{
		{
			src:      filepath.Join(baseDir, "common", "caddy", "Caddyfile"),
			dst:      "Caddyfile",
			sanitize: g.sanitizeText,
		},
		{
			src:      filepath.Join(baseDir, "common", "caddy-config", "caddy", "autosave.json"),
			dst:      "caddy-autosave.json",
			sanitize: g.sanitizeJSON,
		},
	}

	for _, f := range caddyFiles {
		data, err := os.ReadFile(f.src)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Warningf("%s not found (catalog may not be configured)\n", f.src)
			} else {
				logger.Warningf("Failed to read %s: %v\n", f.src, err)
			}

			continue
		}

		g.writeFile(catDir, f.dst, f.sanitize(data))
	}
}

// collectCatalogCredentials saves the CLI credentials file
// (~/.config/ai-services/catalog-credentials.json) with tokens redacted.
func (g *podmanGatherer) collectCatalogCredentials(catDir string) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		logger.Warningf("Cannot determine user config dir: %v\n", err)
		return
	}

	credsPath := filepath.Join(cfgDir, "ai-services", "catalog-credentials.json")

	data, err := os.ReadFile(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warningln("Catalog credentials file not found (not logged in).")
		} else {
			logger.Warningf("Failed to read catalog credentials: %v\n", err)
		}

		return
	}

	g.writeFile(catDir, "catalog-credentials.json", g.sanitizeJSON(data))
}

// ── models info collection ────────────────────────────────────────────────────

// collectModelsInfo records which models are present under <BaseDir>/models/
// and how much disk space each one occupies. Model weights are never copied —
// only the directory listing and per-model disk usage are written.
func (g *podmanGatherer) collectModelsInfo(outDir string) {
	logger.Infoln("Collecting models information…")

	modelsPath := pkgutils.GetModelsPath()

	entries, err := os.ReadDir(modelsPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warningf("Models directory not found at %s\n", modelsPath)
		} else {
			logger.Warningf("Failed to read models directory: %v\n", err)
		}

		return
	}

	modelsDir := filepath.Join(outDir, "models")
	if err := os.MkdirAll(modelsDir, dirPerm); err != nil {
		logger.Warningf("Failed to create models output directory: %v\n", err)
		return
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Models directory: %s", modelsPath))
	lines = append(lines, strings.Repeat("-", 60))

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
			size, fileCount := dirStats(modelPath)
			lines = append(lines, fmt.Sprintf(
				"  %s/%s  (%s, %d files)",
				org.Name(), model.Name(), formatBytes(size), fileCount,
			))
		}
	}

	g.writeFile(modelsDir, "models.txt", []byte(strings.Join(lines, "\n")+"\n"))
}

// dirStats walks dir and returns total byte size and file count.
func dirStats(dir string) (totalBytes int64, fileCount int) {
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		totalBytes += info.Size()
		fileCount++

		return nil
	})

	return totalBytes, fileCount
}

// formatBytes renders a byte count as a human-readable string (GiB / MiB / KiB / B).
func formatBytes(b int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)

	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(mib))
	case b >= kib:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── secret metadata collection ────────────────────────────────────────────────

// collectSecretInfo saves metadata (name, ID, labels, driver, creation time)
// for all Podman secrets. Secret *values* are never exposed by the Podman CLI
// after creation, so this step is safe by design.
//
// `podman secret ls --format json` does not emit real JSON (it prints the
// literal string "json"). We therefore list names via `--noheading` and then
// collect full metadata with `podman secret inspect`, which does emit JSON.
func (g *podmanGatherer) collectSecretInfo(outDir string) {
	logger.Infoln("Collecting secret metadata…")

	secDir := filepath.Join(outDir, "secrets")
	if err := os.MkdirAll(secDir, dirPerm); err != nil {
		logger.Warningf("Failed to create secrets directory: %v\n", err)
		return
	}

	// List names only — `--noheading` gives tab-separated lines: ID\tNAME\t…
	raw, err := podmanRun("secret", "ls", "--noheading")
	if err != nil {
		logger.Warningf("podman secret ls failed: %v\n", err)
		return
	}

	names := parseSecretNames(raw)
	if len(names) == 0 {
		logger.Infoln("No Podman secrets found.")
		return
	}

	// Collect full metadata for all secrets in one inspect call.
	args := append([]string{"secret", "inspect"}, names...)
	inspectRaw, err := podmanRun(args...)
	if err != nil {
		logger.Warningf("podman secret inspect failed: %v\n", err)
		return
	}

	g.writeFile(secDir, "secrets.json", g.sanitizeJSON(inspectRaw))
}

// parseSecretNames extracts secret names from `podman secret ls --noheading`
// output. Each line is tab-separated: ID\tNAME\tDRIVER\tCREATED\tUPDATED.
func parseSecretNames(raw []byte) []string {
	var names []string

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 { //nolint:mnd // col 0=ID, col 1=NAME
			names = append(names, fields[1])
		}
	}

	return names
}

// ── system / network / volume collection ──────────────────────────────────────

func (g *podmanGatherer) collectSystemInfo(outDir string) {
	logger.Infoln("Collecting system information…")

	sysDir := filepath.Join(outDir, "system")
	if err := os.MkdirAll(sysDir, dirPerm); err != nil {
		logger.Warningf("Failed to create system directory: %v\n", err)
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
		raw, err := podmanRun(c.args...)
		if err != nil {
			logger.Warningf("podman %s failed: %v\n", strings.Join(c.args, " "), err)
			continue
		}

		g.writeFile(sysDir, c.filename, g.sanitizeText(raw))
	}
}

func (g *podmanGatherer) collectNetworkInfo(outDir string) {
	logger.Infoln("Collecting network information…")

	netDir := filepath.Join(outDir, "network")
	if err := os.MkdirAll(netDir, dirPerm); err != nil {
		logger.Warningf("Failed to create network directory: %v\n", err)
		return
	}

	raw, err := podmanRun("network", "ls", "--format", "json")
	if err != nil {
		logger.Warningf("podman network ls failed: %v\n", err)
		return
	}

	g.writeFile(netDir, "networks.json", g.sanitizeJSON(raw))
}

func (g *podmanGatherer) collectVolumeInfo(outDir string) {
	logger.Infoln("Collecting volume information…")

	volDir := filepath.Join(outDir, "volumes")
	if err := os.MkdirAll(volDir, dirPerm); err != nil {
		logger.Warningf("Failed to create volumes directory: %v\n", err)
		return
	}

	raw, err := podmanRun("volume", "ls", "--format", "json")
	if err != nil {
		logger.Warningf("podman volume ls failed: %v\n", err)
		return
	}

	g.writeFile(volDir, "volumes.json", g.sanitizeJSON(raw))
}

// ── sanitize helpers ──────────────────────────────────────────────────────────

// sanitizeJSON redacts sensitive keys in a JSON byte slice.
// Falls back to sanitizeText when the input is not valid JSON.
func (g *podmanGatherer) sanitizeJSON(raw []byte) []byte {
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return g.sanitizeText(raw)
	}

	out, err := json.MarshalIndent(sanitizeAny(g.sanitizer, obj), "", "  ")
	if err != nil {
		return raw
	}

	return out
}

// sanitizeText redacts KEY=VALUE patterns line-by-line in plain-text output.
func (g *podmanGatherer) sanitizeText(raw []byte) []byte {
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		lines[i] = redactLine(g.sanitizer, line)
	}

	return []byte(strings.Join(lines, "\n"))
}

// sanitizeAny recursively sanitizes maps inside an arbitrary JSON-decoded value.
func sanitizeAny(s *sanitize.SecretSanitizer, v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return s.SanitizeArgs([]any{typed})[0]
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeAny(s, item)
		}

		return out
	default:
		return v
	}
}

// redactLine redacts the value side of a KEY=VALUE line when the key is sensitive.
func redactLine(s *sanitize.SecretSanitizer, line string) string {
	const kvParts = 2

	kv := strings.SplitN(line, "=", kvParts)
	if len(kv) != kvParts {
		return line
	}

	result := s.SanitizeArgs([]any{map[string]any{kv[0]: kv[1]}})
	if m, ok := result[0].(map[string]any); ok {
		return kv[0] + "=" + fmt.Sprintf("%v", m[kv[0]])
	}

	return line
}

// ── file I/O ──────────────────────────────────────────────────────────────────

func (g *podmanGatherer) writeFile(dir, filename string, content []byte) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, filePerm); err != nil {
		logger.Warningf("Failed to write %s: %v\n", path, err)
	}
}

// ── podman CLI helpers ────────────────────────────────────────────────────────

// podmanRun executes `podman <args>` and returns combined stdout+stderr.
func podmanRun(args ...string) ([]byte, error) {
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("podman %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return out, nil
}

// podmanContainerName extracts the container name from a `podman ps --format json` entry.
// The "Names" field is a JSON array of strings.
func podmanContainerName(c map[string]any) string {
	switch v := c["Names"].(type) {
	case []any:
		if len(v) > 0 {
			return fmt.Sprintf("%v", v[0])
		}
	case string:
		return v
	}

	return ""
}
