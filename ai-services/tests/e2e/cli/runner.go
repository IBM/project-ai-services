package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

// Service name substrings used to identify catalog URLs in 'application info' output.
// Each constant matches the stable hostname prefix of the corresponding deployed service,
// e.g. "https://chat-bot-backend-<slug>.<ip>.nip.io". Using constants here means a
// service rename only requires a single edit.
const (
	svcChatBotBackend  = "chat-bot-backend"
	svcChatBotUI       = "chat-bot-ui"
	svcDigitizeBackend = "digitize-backend"
	svcSimilarityAPI   = "similarity-api"
)

// ptyWinRows and ptyWinCols define the PTY window size used by runWithPTY.
// These are fixed terminal dimensions for interactive CLI prompts — not magic numbers.
const (
	ptyWinRows = 24 //nolint:mnd
	ptyWinCols = 80 //nolint:mnd
)

type CreateOptions struct {
	SkipImageDownload bool
	SkipModelDownload bool
	SkipValidation    string
	Verbose           bool
	ImagePullPolicy   string
}

type StartOptions struct {
	Pod        string
	SkipLogs   bool
	IngestDocs bool
}

// runCLI executes cfg.AIServiceBin with the given args, returning combined output.
// On a non-zero exit the error is wrapped as "<errLabel> failed: <err>\n<output>".
// This eliminates the repeated exec.CommandContext / CombinedOutput / fmt.Errorf
// boilerplate that would otherwise appear in every runner function.
func runCLI(ctx context.Context, cfg *config.Config, errLabel string, args ...string) (string, error) {
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("%s failed: %w\n%s", errLabel, err, output)
	}

	return output, nil
}

// isKnownSpyreConfigureFailure reports whether a bootstrap configure/bootstrap
// output contains the known Spyre post-repair strings. When this returns true
// the OS-level exit error is suppressed. These strings mean that configure
// attempted automatic repairs (VFIO permissions, SELinux policy via semodule,
// udev rules) but the post-repair re-validation checks still did not pass.
// This is a Spyre-hardware-specific failure that does not affect the
// application-layer tests; all subsequent test steps proceed normally.
func isKnownSpyreConfigureFailure(output string) bool {
	return strings.Contains(output, "some Spyre configuration checks still failed after repair") ||
		strings.Contains(output, "failed to configure spyre card")
}

// Bootstrap runs the full bootstrap (configure + validate).
func Bootstrap(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	output, err := runCLI(ctx, cfg, "bootstrap", "bootstrap", "--runtime", appRuntime)
	if err != nil {
		// For podman, 'bootstrap' (full run: configure + validate) also exits non-zero
		// when Spyre post-repair checks still fail — same acceptable state.
		if appRuntime == "podman" && isKnownSpyreConfigureFailure(output) {
			logger.Infof("[CLI] bootstrap exited non-zero with known Spyre repair state — treating as non-fatal")

			return output, nil
		}

		return output, err
	}

	return output, nil
}

// BootstrapConfigure runs only the 'configure' step.
// For podman, the command exits non-zero when Spyre post-repair checks still fail.
// That is expected behaviour — repairs were applied, a reboot may be needed for full
// effect. We suppress the OS-level exit error for the two known acceptable Spyre
// strings so tests can continue evaluating the output via ValidateBootstrapConfigureOutput
// without a hard failure on the raw exec error.
func BootstrapConfigure(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	output, err := runCLI(ctx, cfg, "bootstrap configure", "bootstrap", "configure", "--runtime", appRuntime)
	if err != nil {
		if appRuntime == "podman" && isKnownSpyreConfigureFailure(output) {
			logger.Infof("[CLI] bootstrap configure exited non-zero with known Spyre repair state — treating as non-fatal")

			return output, nil
		}

		return output, err
	}

	return output, nil
}

// BootstrapValidate runs only the 'validate' step.
func BootstrapValidate(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "bootstrap validate", "bootstrap", "validate", "--runtime", appRuntime)
}

// CreateApp creates an application via the CLI.
func CreateApp(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	template string,
	params string,
	opts CreateOptions,
	appRuntime string,
) (string, error) {
	args := []string{
		"application", "create", appName,
		"-t", template,
	}
	if params != "" {
		args = append(args, "--params", params)
	}
	if opts.SkipImageDownload {
		args = append(args, "--skip-image-download")
	}
	if opts.SkipModelDownload {
		args = append(args, "--skip-model-download")
	}
	if opts.SkipValidation != "" {
		args = append(args, "--skip-validation", opts.SkipValidation)
	}
	if opts.ImagePullPolicy != "" {
		args = append(args, "--image-pull-policy", opts.ImagePullPolicy)
	}
	args = append(args, "--runtime", appRuntime)

	return runCLI(ctx, cfg, "application create", args...)
}

// newRAGHTTPClient returns an HTTP client for RAG health-check probes with TLS skipped when needed.
func newRAGHTTPClient(appRuntime string, isCatalogPath bool, timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

	if appRuntime == "openshift" || isCatalogPath {
		reason := "catalog path — nip.io self-signed certificate"
		if appRuntime == "openshift" {
			reason = "OpenShift runtime"
		}
		logger.Warningf("[WARNING] TLS certificate verification disabled (%s)", reason)
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}

	return client
}

// CreateRAGAppAndValidate creates a RAG application, probes health endpoints, and validates output.
func CreateRAGAppAndValidate(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	template string,
	params string,
	backendPort string,
	uiPort string,
	opts CreateOptions,
	pods []string,
	appRuntime string,
) (string, error) {
	const (
		maxRetries            = 10               //nolint:mnd
		waitTime              = 15 * time.Second //nolint:mnd
		defaultCommandTimeout = 10 * time.Second //nolint:mnd
	)

	output, err := CreateApp(ctx, cfg, appName, template, params, opts, appRuntime)
	if err != nil {
		return output, err
	}

	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return output, err
	}

	backendURL, chatbotUiURL, isCatalogPath, err := getRAGURLs(ctx, cfg, appRuntime, appName, output, backendPort, uiPort)
	if err != nil {
		return output, err
	}

	httpClient := newRAGHTTPClient(appRuntime, isCatalogPath, defaultCommandTimeout)

	for _, ep := range []string{"/health", "/v1/models", "/db-status"} {
		if err := waitForEndpointOK(httpClient, backendURL+ep, maxRetries, waitTime); err != nil {
			return output, err
		}
	}

	logger.Infof("[UI] Chatbot UI available at: %s", chatbotUiURL)

	return output, nil
}

// getRAGURLs returns backend and UI URLs for a deployed RAG application.
// For podman, URLs come from 'application info'; for openshift from the create output.
func getRAGURLs(ctx context.Context, cfg *config.Config, appRuntime, appName, createOutput, backendPort, uiPort string) (backendURL, uiURL string, isCatalogPath bool, err error) {
	if appRuntime == "openshift" {
		urls := ExtractURLsFromOutput(createOutput)
		bURL := strings.Replace(urls[0], "digitize-ui", "backend", 1)
		uURL := strings.Replace(urls[0], "digitize-ui", "ui", 1)

		return bURL, uURL, false, nil
	}

	// Podman catalog path: fetch info output which contains all service URLs via info.md.
	infoOutput, infoErr := ApplicationInfo(ctx, cfg, appName, appRuntime)
	if infoErr != nil {
		return "", "", true, fmt.Errorf("could not retrieve application info for URL extraction: %w", infoErr)
	}

	bURL, uURL := extractCatalogRAGURLs(infoOutput)
	if bURL == "" {
		// Log full info output to help diagnose URL format changes.
		logger.Warningf("[RAG] Could not extract chat backend URL from 'application info' output:\n%s", infoOutput)

		return "", "", true, fmt.Errorf("could not determine RAG backend URL from 'application info' output")
	}

	return bURL, uURL, true, nil
}

// extractCatalogRAGURLs extracts the chat backend and UI URLs from 'application info' output.
// Matches by URL host substring — robust against info.md title changes.
func extractCatalogRAGURLs(output string) (string, string) {
	return extractURLBySubstring(output, svcChatBotBackend),
		extractURLBySubstring(output, svcChatBotUI)
}

// extractHTTPSURL extracts the first https:// URL from a line, stripping trailing punctuation.
func extractHTTPSURL(line string) string {
	const httpsPrefix = "https://"
	idx := strings.Index(line, httpsPrefix)
	if idx < 0 {
		return ""
	}

	rest := line[idx:]

	// Stop at the first whitespace — nothing after a space is part of the URL.
	if spaceIdx := strings.IndexAny(rest, " \t"); spaceIdx >= 0 {
		rest = rest[:spaceIdx]
	}

	// Strip any trailing punctuation left over (e.g. a period before the space).
	rest = strings.TrimRight(rest, ".,;")

	return rest
}

// extractURLBySubstring returns the first HTTPS URL in output whose value contains substr.
func extractURLBySubstring(output, substr string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if url := extractHTTPSURL(line); url != "" && strings.Contains(url, substr) {
			return url
		}
	}

	return ""
}

// waitForEndpointOK polls the given endpoint until it returns HTTP 200 OK or exhausts retries.
func waitForEndpointOK(
	client *http.Client,
	endpoint string,
	maxRetries int,
	waitTime time.Duration,
) error {
	var lastErr error
	for i := 1; i <= maxRetries; i++ {
		resp, err := client.Get(endpoint)
		if err == nil && resp.StatusCode == http.StatusOK {
			if cerr := resp.Body.Close(); cerr != nil {
				logger.Warningf("[WARNING] failed to close response body for %s: %v", endpoint, cerr)
			}
			logger.Infof("[RAG] GET %s -> 200 OK", endpoint)

			return nil
		}
		if resp != nil {
			if cerr := resp.Body.Close(); cerr != nil {
				logger.Warningf("[WARNING] failed to close response body for %s: %v", endpoint, cerr)
			}
		}
		lastErr = err
		logger.Infof(
			"[RAG] Waiting for %s (attempt %d/%d)",
			endpoint, i, maxRetries,
		)
		time.Sleep(waitTime)
	}

	return fmt.Errorf("endpoint %s failed after retries: %w", endpoint, lastErr)
}

// GetBaseURL extracts the RAG chat-backend URL from CLI output.
// For podman uses host-substring matching; for OpenShift uses regex.
func GetBaseURL(createOutput string, backendPort string) (string, error) {
	// Catalog path (podman): extract chat-bot-backend HTTPS URL from info output.
	if backendURL, _ := extractCatalogRAGURLs(createOutput); backendURL != "" {
		return backendURL, nil
	}

	// OpenShift path: extract any https/http URL from the output.
	urls := ExtractURLsFromOutput(createOutput)
	if len(urls) > 0 {
		return urls[0], nil
	}

	return "", fmt.Errorf("could not determine base URL from CLI output")
}

// GetJudgeBaseURL returns the base URL for the local LLM-as-Judge container.
func GetJudgeBaseURL(judgePort string) string {
	return fmt.Sprintf("http://localhost:%s", judgePort)
}

// ExtractCatalogDigitizeURL extracts the digitize-backend URL from 'application info' output.
func ExtractCatalogDigitizeURL(infoOutput string) string {
	return extractURLBySubstring(infoOutput, svcDigitizeBackend)
}

// ExtractSimilarityAPIURL extracts the similarity-api URL from 'application info' output.
// Falls back to legacy plain-HTTP extraction for non-catalog podman environments.
func ExtractSimilarityAPIURL(infoOutput string) string {
	// Catalog path: HTTPS nip.io URL with "similarity-api" in the host.
	if url := extractURLBySubstring(infoOutput, svcSimilarityAPI); url != "" {
		return url
	}

	// Legacy podman path: plain http URL on the line containing "Similarity API".
	for _, line := range strings.Split(infoOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "Similarity API") {
			continue
		}
		for _, u := range ExtractURLsFromOutput(line) {
			if strings.HasPrefix(u, "http://") {
				return u
			}
		}
	}

	return ""
}

// WaitForApplicationInfoURLs polls 'application info' until service URLs are present.
// For podman requires both chat-bot-backend and similarity-api; for openshift any URL suffices.
func WaitForApplicationInfoURLs(ctx context.Context, cfg *config.Config, appName, appRuntime string, maxWait, pollInterval time.Duration) (string, error) {
	deadline := time.Now().Add(maxWait)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		infoOutput, infoErr := ApplicationInfo(ctx, cfg, appName, appRuntime)
		if infoErr != nil {
			logger.Warningf("[WAIT] application info attempt %d failed: %v — retrying", attempt, infoErr)
			time.Sleep(pollInterval)

			continue
		}
		if appRuntime == "podman" {
			backendURL, _ := extractCatalogRAGURLs(infoOutput)
			similarityURL := ExtractSimilarityAPIURL(infoOutput)
			if backendURL != "" && similarityURL != "" {
				logger.Infof("[WAIT] application info URLs ready after %d attempt(s) — backend: %s, similarity: %s",
					attempt, backendURL, similarityURL)

				return infoOutput, nil
			}
		} else {
			if len(ExtractURLsFromOutput(infoOutput)) > 0 {
				return infoOutput, nil
			}
		}
		logger.Infof("[WAIT] application info attempt %d: URLs not yet present (pods may still be starting), retrying in %s", attempt, pollInterval)
		time.Sleep(pollInterval)
	}
	infoOutput, _ := ApplicationInfo(ctx, cfg, appName, appRuntime)

	return infoOutput, fmt.Errorf("timed out waiting for application info URLs after %s (%d attempts)", maxWait, attempt)
}

// HelpCommand runs the 'help' command with or without arguments.
func HelpCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	return runCLI(ctx, cfg, "help command run", args...)
}

// ApplicationPS runs the application ps command.
func ApplicationPS(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
	flags ...string,
) (string, error) {
	args := []string{"application", "ps"}

	if appName != "" {
		args = append(args, appName)
	}

	args = append(args, flags...)
	args = append(args, "--runtime", appRuntime)

	return runCLI(ctx, cfg, "application ps", args...)
}

// ListImage lists images for the given application template.
func ListImage(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) error {
	output, err := runCLI(ctx, cfg, "list images", "application", "image", "list", "--template", templateName, "--runtime", appRuntime)
	if err != nil {
		return err
	}

	return ValidateImageListOutput(output, appRuntime)
}

// PullImage pulls images for the given application template.
func PullImage(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) error {
	url, uname, pswd := bootstrap.GetPodManCreds()
	if err := bootstrap.PodmanRegistryLogin(url, uname, pswd); err != nil {
		return fmt.Errorf("pull images failed due to podman login err: %w", err)
	}

	url, uname, pswd = bootstrap.GetRHRegistryCreds()
	if err := bootstrap.PodmanRegistryLogin(url, uname, pswd); err != nil {
		return fmt.Errorf("pull images failed due to podman login err: %w", err)
	}

	output, err := runCLI(ctx, cfg, "pull images", "application", "image", "pull", "--template", templateName, "--runtime", appRuntime)
	if err != nil {
		return err
	}

	return ValidatePullImageOutput(output, templateName, appRuntime)
}

// StopAppWithPods stops an application specifying pods to stop.
func StopAppWithPods(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	pods []string,
	appRuntime string,
) (string, error) {
	args := []string{
		"application", "stop", appName,
		"--pod", strings.Join(pods, ","),
		"--yes",
		"--runtime", appRuntime,
	}

	output, err := runCLI(ctx, cfg, "application stop --pod", args...)
	if err != nil {
		return output, err
	}

	if appRuntime == "openshift" {
		return output, ValidateStopAppOutputOpenshift(output)
	}

	if err := ValidateStopAppOutputPodman(output); err != nil {
		return output, err
	}

	psOutput, err := ApplicationPS(ctx, cfg, appName, appRuntime)
	if err != nil {
		return output, err
	}

	if err := ValidatePodsExitedAfterStop(psOutput, appName, appRuntime); err != nil {
		return output, err
	}

	return output, nil
}

// StartApplication starts an application and validates the output.
func StartApplication(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
	opts StartOptions,
) (string, error) {
	args := []string{"application", "start", appName, "--yes"}

	if opts.Pod != "" {
		args = append(args, "--pod="+opts.Pod)
	}
	if opts.SkipLogs {
		args = append(args, "--skip-logs")
	}

	args = append(args, "--runtime", appRuntime)

	output, err := runCLI(ctx, cfg, "application start", args...)
	logger.Infof("[CLI] Output: %s", output)

	if err != nil {
		return output, err
	}

	if appRuntime == "openshift" {
		return output, ValidateStartAppOutputOpenshift(output)
	}

	if err := ValidateStartAppOutput(output); err != nil {
		return output, err
	}

	psOutput, err := ApplicationPS(ctx, cfg, appName, appRuntime)
	if err != nil {
		return output, err
	}

	if err := ValidatePodsRunningAfterStart(psOutput, appName, appRuntime); err != nil {
		return output, err
	}

	return output, nil
}

// DeleteAppSkipCleanup deletes an application with --skip-cleanup flag.
func DeleteAppSkipCleanup(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
) (string, error) {
	args := []string{
		"application", "delete", appName,
		"--skip-cleanup",
		"--yes",
		"--runtime", appRuntime,
	}

	output, err := runCLI(ctx, cfg, "application delete --skip-cleanup", args...)
	if err != nil {
		return output, err
	}

	if err := ValidateDeleteAppOutput(output, appName); err != nil {
		return output, err
	}

	time.Sleep(common.DeleteSleepInterval)

	psOutput, err := ApplicationPS(ctx, cfg, appName, appRuntime)
	if err != nil {
		// "not found" means the application was already removed — treat as success.
		if strings.Contains(err.Error(), "not found") {
			logger.Infof("[TEST] Application %s no longer exists after delete (not found) — OK", appName)

			return output, nil
		}

		return output, err
	}
	if err := ValidateNoPodsAfterDelete(psOutput); err != nil {
		return output, err
	}

	return output, nil
}

// ApplicationInfo runs the 'application info' command.
func ApplicationInfo(ctx context.Context, cfg *config.Config, appName string, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "application info", "application", "info", appName, "--runtime", appRuntime)
}

// ModelList lists models for a given application template.
func ModelList(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "application model list", "application", "model", "list", "--template", templateName, "--runtime", appRuntime)
}

// ModelDownload downloads models for a given application template.
func ModelDownload(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) (string, error) {
	if err := common.EnsureDir(utils.GetModelsPath()); err != nil {
		return "", err
	}

	return runCLI(ctx, cfg, "application model download", "application", "model", "download", "--template", templateName, "--runtime", appRuntime)
}

// TemplatesCommand runs the 'application template' command.
func TemplatesCommand(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "application templates command run", "application", "templates", "--runtime", appRuntime)
}

// CatalogConfigure deploys or ensures the catalog service is running.
// Uses a PTY so the password prompt on first run can be satisfied non-interactively.
func CatalogConfigure(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	password := bootstrap.GetCatalogAdminPassword()
	if password == "" {
		return "", fmt.Errorf("CATALOG_PASSWORD environment variable is not set")
	}

	args := []string{"catalog", "configure", "--runtime", appRuntime}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	output, err := runWithPTY(ctx, cfg.AIServiceBin, args, password+"\n"+password+"\n")
	if err != nil {
		return output, fmt.Errorf("catalog configure failed: %w\n%s", err, output)
	}

	return output, nil
}

// runWithPTY starts cmd in a PTY, writes input to the master, and returns all output.
func runWithPTY(ctx context.Context, bin string, args []string, input string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)

	ptmx, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: ptyWinRows, Cols: ptyWinCols}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to start PTY: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	if _, err := ptmx.Write([]byte(input)); err != nil {
		logger.Warningf("[CLI] PTY write warning: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, ptmx)

	if err := cmd.Wait(); err != nil {
		return buf.String(), err
	}

	return buf.String(), nil
}

// CatalogUninstall removes the catalog service and all associated resources.
func CatalogUninstall(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	output, err := runCLI(ctx, cfg, "catalog uninstall", "catalog", "uninstall", "--runtime", appRuntime, "--yes")
	if err != nil {
		return output, err
	}

	if err := ValidateCatalogUninstallOutput(output); err != nil {
		return output, err
	}

	return output, nil
}

// CatalogInfo runs 'ai-services catalog info' and returns the combined output.
func CatalogInfo(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog info", "catalog", "info", "--runtime", appRuntime)
}

// ExtractCatalogBackendURL extracts the Catalog Backend API URL from 'catalog info' output.
func ExtractCatalogBackendURL(infoOutput string) string {
	const backendMarker = "Catalog Backend API is available at "
	for _, line := range strings.Split(infoOutput, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, backendMarker); idx >= 0 {
			return strings.TrimSpace(line[idx+len(backendMarker):])
		}
	}

	return ""
}

// ExtractCatalogBackendURLFromConfigureOutput extracts the Catalog Backend URL from 'catalog configure' output.
func ExtractCatalogBackendURLFromConfigureOutput(configureOutput string) string {
	const backendMarker = "Access the Catalog Backend at "
	for _, line := range strings.Split(configureOutput, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, backendMarker)
		if idx >= 0 {
			return strings.TrimRight(strings.TrimSpace(line[idx+len(backendMarker):]), " .,")
		}
	}

	return ExtractCatalogBackendURL(configureOutput)
}

// CatalogLogin runs a non-interactive catalog login, piping the password via stdin.
func CatalogLogin(ctx context.Context, cfg *config.Config, serverURL, username, password, appRuntime string, insecure bool) (string, error) {
	args := []string{
		"catalog", "login",
		"--server", serverURL,
		"--username", username,
		"--password-stdin",
		"--runtime", appRuntime,
	}
	if insecure {
		args = append(args, "--insecure")
	}
	logger.Infof("[CLI] Running: %s catalog login --server %s --username %s --password-stdin --runtime %s (insecure=%v)",
		cfg.AIServiceBin, serverURL, username, appRuntime, insecure)
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	cmd.Stdin = bytes.NewBufferString(password + "\n")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return "", fmt.Errorf("catalog login failed: %w", err)
	}

	return output, nil
}

// VersionCommand runs the 'version' command.
func VersionCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	return runCLI(ctx, cfg, "version command run", args...)
}

// GitVersionCommands runs the git commands required for version check.
func GitVersionCommands(ctx context.Context) (string, string, error) {
	versionArgs := strings.Split("describe --tags --always", " ")
	commitArgs := strings.Split("rev-parse --short HEAD", " ")

	logger.Infof("[CLI] Running: git %v", versionArgs)
	vcmd := exec.CommandContext(ctx, "git", versionArgs...)
	vout, err := vcmd.CombinedOutput()
	voutput := string(vout)
	if err != nil {
		return "", "", fmt.Errorf("git version command run failed: %w", err)
	}

	logger.Infof("[CLI] Running: git %v", commitArgs)
	ccmd := exec.CommandContext(ctx, "git", commitArgs...)
	cout, err := ccmd.CombinedOutput()
	coutput := string(cout)
	if err != nil {
		return voutput, "", fmt.Errorf("git commit command run failed: %w", err)
	}

	return voutput, coutput, nil
}

// ApplicationLogs fetches logs for a specific pod and container.
func ApplicationLogs(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	podName string,
	containerNameOrID string,
	appRuntime string,
) (string, error) {
	args := []string{
		"application", "logs", appName,
		"--pod", podName,
	}
	if containerNameOrID != "" {
		args = append(args, "--container", containerNameOrID)
	}

	args = append(args, "--runtime", appRuntime)
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}

		return buf.String(), nil

	case err := <-done:
		output := buf.String()
		if err != nil {
			return output, fmt.Errorf("application logs failed: %w\n%s", err, output)
		}

		return output, nil
	}
}

// ExtractURLsFromOutput returns all http/https URLs found in output.
func ExtractURLsFromOutput(output string) []string {
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	matches := urlRegex.FindAllString(output, -1)

	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		cleanURL := strings.TrimRight(match, ".,;:!?")
		urls = append(urls, cleanURL)
	}

	return urls
}
