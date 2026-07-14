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
// output contains the known "Spyre post-repair checks still failing" strings.
// When this returns true the OS-level exit error can be suppressed — the repairs
// were applied (VFIO permissions + SELinux policy were fixed) but a reboot is
// needed for the changes to be fully effective. Application creation and all
// other tests proceed normally on this hardware state.
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

// CreateRAGAppAndValidate creates an application, waits for health checks, and validates RAG endpoints.
// NOTE: This is intentionally RAG-specific and used only by RAG E2E tests.
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
		maxRetries            = 10
		waitTime              = 15 * time.Second
		defaultCommandTimeout = 10 * time.Second
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

	// Skip TLS verification for:
	//   - OpenShift (self-signed certificates)
	//   - Podman catalog path (nip.io self-signed certificates via Caddy)
	skipTLSVerify := appRuntime == "openshift" || isCatalogPath
	httpClient := &http.Client{
		Timeout: defaultCommandTimeout,
	}
	if skipTLSVerify {
		logger.Warningf("[WARNING] TLS certificate verification disabled (%s)", func() string {
			if appRuntime == "openshift" {
				return "OpenShift runtime"
			}
			return "catalog path — nip.io self-signed certificate"
		}())
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}
	endpoints := []string{
		"/health",
		"/v1/models",
		"/db-status",
	}
	for _, ep := range endpoints {
		fullURL := backendURL + ep
		if err := waitForEndpointOK(httpClient, fullURL, maxRetries, waitTime); err != nil {
			return output, err
		}
	}
	logger.Infof("[UI] Chatbot UI available at: %s", chatbotUiURL)

	return output, nil
}

// getRAGURLs extracts the backend and UI URLs for a deployed RAG application.
//
// For podman (catalog path): the 'application create' next.md output only contains digitize URLs.
// The chat backend and UI URLs are only available in 'application info' (info.md), which prints:
//
//	"- chat is available to use at https://chat-bot-ui-<slug>.<domain>"
//	"- chat API is available to use at https://chat-bot-backend-<slug>.<domain>"
//
// So we call 'application info' to get the authoritative URLs.
//
// For openshift: extract route URLs directly from the create output.
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

// extractCatalogRAGURLs parses the 'application info' output (info.md rendered) for the
// chat service backend API URL and chat UI URL.
//
// The catalog renders human-readable service titles from the template catalog, e.g.:
//
//	"- Question and answer is available to use at https://chat-bot-ui-<slug>.<domain>"
//	"- Question and answer API is available to use at https://chat-bot-backend-<slug>.<domain>"
//
// We identify the chat UI line as: contains "is available to use at" AND NOT "API"
// AND the URL host contains "chat-bot-ui".
// We identify the chat backend line as: contains "API is available to use at"
// AND the URL host contains "chat-bot-backend".
//
// Using URL-host matching (chat-bot-ui / chat-bot-backend) makes this robust against
// any future title-text changes in info.md.
//
// Returns (backendURL, uiURL) — empty strings if not found.
func extractCatalogRAGURLs(output string) (string, string) {
	return extractURLBySubstring(output, "chat-bot-backend"),
		extractURLBySubstring(output, "chat-bot-ui")
}

// extractHTTPSURL extracts the first https:// URL from a line of text.
// A URL ends at the first whitespace character — everything after a space is
// not part of the URL (e.g. ". Use this endpoint..." on the same line).
// Trailing punctuation (period, comma) immediately before whitespace is also stripped.
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

// extractURLBySubstring scans output line-by-line and returns the first HTTPS
// URL whose value contains substr. Returns "" when no matching URL is found.
// This is the shared building block for all single-URL catalog extraction helpers.
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

// GetBaseURL extracts the chat-backend URL from the CLI output (for ragBaseURL).
// For podman (catalog path) the output contains HTTPS domain URLs from info.md — extracted by host substring.
// For OpenShift the output contains route URLs — extracted by regex.
//
// NOTE: For the LLM-as-Judge URL (judgeBaseURL), the judge is a separate local podman
// container on localhost:<port> — use GetJudgeBaseURL instead.
// NOTE: For the digitize backend URL, use ExtractCatalogDigitizeURL instead.
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
// The judge is a local podman container bound to localhost:<judgePort>, not a
// catalog-deployed service, so its URL is always http://localhost:<port>.
func GetJudgeBaseURL(judgePort string) string {
	return fmt.Sprintf("http://localhost:%s", judgePort)
}

// ExtractCatalogDigitizeURL parses the 'application info' output for the
// digitize-backend service URL.
//
// Actual output line:
//
//	"- Digitize documents Documents API is available to use at https://digitize-backend-<slug>.<domain>."
//
// We match on URL-host substring "digitize-backend" which is stable regardless
// of human-readable title changes in info.md.
func ExtractCatalogDigitizeURL(infoOutput string) string {
	return extractURLBySubstring(infoOutput, "digitize-backend")
}

// ExtractSimilarityAPIURL extracts the similarity-api URL from 'application info' output.
//
// Catalog path (podman): URL host contains "similarity-api"
//
//	e.g. "https://similarity-api-<slug>.<ip>.nip.io"
//
// Legacy podman path: plain http URL with HOST_IP:PORT
//
//	e.g. "http://10.48.64.172:9100"  (extracted by ExtractURLsFromOutput fallback)
func ExtractSimilarityAPIURL(infoOutput string) string {
	// Catalog path: HTTPS nip.io URL with "similarity-api" in the host.
	if url := extractURLBySubstring(infoOutput, "similarity-api"); url != "" {
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

// WaitForApplicationInfoURLs polls 'application info' until the catalog backend URL
// (chat-bot-backend) is present in the output — meaning pods are healthy and the
// info.md template has rendered the running branch.
//
// This is needed because after 'application start' the containers may take some
// time to become healthy, during which getContainerStatus returns empty strings and
// info.md renders the "unavailable" branch (no URLs). We must wait for URLs to
// appear before the RAG/Digitize BeforeAll blocks attempt to use them.
//
// maxWait is the total polling duration; pollInterval is the sleep between attempts.
// Returns the info output once the backend URL is present, or an error on timeout.
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
		// For podman, require both chat-bot-backend AND similarity-api URLs to be
		// present — similarity-api is a hard dependency of every RAG query and must
		// be healthy before evaluation starts.
		// For openshift, any URL in the output is sufficient.
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
	// Last attempt — return whatever we have even if URLs are missing so the
	// caller can surface a more descriptive error.
	infoOutput, _ := ApplicationInfo(ctx, cfg, appName, appRuntime)
	return infoOutput, fmt.Errorf("timed out waiting for application info URLs after %s (%d attempts)", maxWait, attempt)
}

// HelpCommand runs the 'help' command with or without arguments.
func HelpCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	return runCLI(ctx, cfg, "help command run", args...)
}

// ApplicationPS runs the 'application ps' command to list application pods.
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

// ListImage from the given application template.
func ListImage(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) error {
	output, err := runCLI(ctx, cfg, "list images", "application", "image", "list", "--template", templateName, "--runtime", appRuntime)
	if err != nil {
		return err
	}

	return ValidateImageListOutput(output, appRuntime)
}

// PullImage from the given application template.
func PullImage(ctx context.Context, cfg *config.Config, templateName string, appRuntime string) error {
	// perform ICR login
	url, uname, pswd := bootstrap.GetPodManCreds()
	if err := bootstrap.PodmanRegistryLogin(url, uname, pswd); err != nil {
		return fmt.Errorf("pull images failed due to podman login err: %w", err)
	}

	// perform RH registry login
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

// StartApplication starts an application's pods and validates the output.
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

	// Validate output.
	if appRuntime == "openshift" {
		return output, ValidateStartAppOutputOpenshift(output)
	}

	if err := ValidateStartAppOutput(output); err != nil {
		return output, err
	}

	// Verify pods are running again.
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

	time.Sleep(common.DeleteSleepInterval) //wait for 10 seconds before checking ps output

	psOutput, err := ApplicationPS(ctx, cfg, appName, appRuntime)
	if err != nil {
		// "not found" means the application record itself is gone — that is the
		// expected state after a successful delete, so treat it as success.
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

// ModelDownload downloads a model for a given application template.
// It ensures the default models directory exists before invoking the CLI so that
// the podman bind-mount does not fail with "no such file or directory".
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

// CatalogConfigure runs 'ai-services catalog configure --runtime <runtime>' to deploy/ensure
// the catalog service is running. It is idempotent — safe to call even if already deployed.
//
// On first run the CLI prompts for an admin password via term.ReadPassword which requires
// a real TTY. We launch the process inside a pseudo-terminal (PTY) so the prompt succeeds,
// then write the password twice (password + confirm) through the PTY master.
// On subsequent runs the catalog-secret already exists and the prompt is skipped entirely.
func CatalogConfigure(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	password := bootstrap.GetCatalogAdminPassword()
	args := []string{"catalog", "configure", "--runtime", appRuntime}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	output, err := runWithPTY(ctx, cfg.AIServiceBin, args, password+"\n"+password+"\n")
	if err != nil {
		return output, fmt.Errorf("catalog configure failed: %w\n%s", err, output)
	}

	return output, nil
}

// runWithPTY starts cmd inside a pseudo-terminal, writes input to the PTY master,
// collects all output, and waits for the process to finish.
// It respects ctx cancellation — the child process is killed when ctx is done.
func runWithPTY(ctx context.Context, bin string, args []string, input string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)

	// Start the command inside a PTY.
	ptmx, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 24, Cols: 80}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to start PTY: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Kill the child on context cancellation.
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	// Write the password(s) to the PTY master immediately.
	// The CLI reads them via term.ReadPassword on the PTY slave (its stdin).
	if _, err := ptmx.Write([]byte(input)); err != nil {
		// Non-fatal if the process already exited before we write.
		logger.Warningf("[CLI] PTY write warning: %v", err)
	}

	// Read all output from the PTY master until it closes (EOF = process exited).
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, ptmx)

	// Wait for the process to exit.
	if err := cmd.Wait(); err != nil {
		return buf.String(), err
	}

	return buf.String(), nil
}

// CatalogUninstall runs 'ai-services catalog uninstall --runtime <runtime> --yes'
// to remove the catalog service and all associated resources (pods, secrets, db data).
// --yes suppresses the interactive confirmation prompt.
// Only supported for podman runtime — openshift returns an error from the CLI.
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
// The output contains the Catalog Backend API URL printed by the configure command.
func CatalogInfo(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog info", "catalog", "info", "--runtime", appRuntime)
}

// ExtractCatalogBackendURL parses the output of 'catalog info' and returns the
// Catalog Backend API URL (https://...).
// The info.md template prints a line like:
//
//	"- Catalog Backend API is available at https://<domain>[:<port>]"
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

// ExtractCatalogBackendURLFromConfigureOutput parses the output of 'catalog configure'
// and returns the Catalog Backend API URL.
// The next.md template prints a line like:
//
//	"- Access the Catalog Backend at https://<domain>[:<port>]"
func ExtractCatalogBackendURLFromConfigureOutput(configureOutput string) string {
	const backendMarker = "Access the Catalog Backend at "
	for _, line := range strings.Split(configureOutput, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, backendMarker)
		if idx >= 0 {
			return strings.TrimRight(strings.TrimSpace(line[idx+len(backendMarker):]), " .,")
		}
	}

	// Fallback: also try info.md marker in case configure output format differs
	return ExtractCatalogBackendURL(configureOutput)
}

// CatalogLogin performs a non-interactive catalog login using server URL, username, and password.
// It runs: ai-services catalog login --server <url> --username <user> --password-stdin [--insecure] --runtime <runtime>
// with the password piped via stdin so no credentials appear in process arguments.
// Pass insecure=true when the catalog server uses a self-signed or nip.io certificate (e2e environments).
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
	// Pipe password via stdin so it never appears in the process argument list.
	cmd.Stdin = bytes.NewBufferString(password + "\n")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("catalog login failed: %w\n%s", err, output)
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
		return voutput, "", fmt.Errorf("git version command run failed: %w\n%s", err, voutput)
	}

	logger.Infof("[CLI] Running: git %v", commitArgs)
	ccmd := exec.CommandContext(ctx, "git", commitArgs...)
	cout, err := ccmd.CombinedOutput()
	coutput := string(cout)
	if err != nil {
		return voutput, coutput, fmt.Errorf("git commit command run failed: %w\n%s", err, coutput)
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

func ExtractURLsFromOutput(output string) []string {
	// Regular expression to match URLs (http and https)
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)

	matches := urlRegex.FindAllString(output, -1)

	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		cleanURL := strings.TrimRight(match, ".,;:!?")
		urls = append(urls, cleanURL)
	}

	return urls
}
