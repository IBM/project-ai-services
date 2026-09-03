package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

// Catalog service ID and endpoint type constants used to look up URLs via the catalog API.
// These match the catalog_id values in the service metadata YAML files and the route
// type field written to the database by the podman deployer.
const (
	catalogSvcChat       = "chat"
	catalogSvcDigitize   = "digitize"
	catalogSvcSimilarity = "similarity"
	endpointTypeAPI      = "api"
	endpointTypeUI       = "ui"
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
	svcSummarizeAPI    = "summarize-api"
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
		maxRetries            = 30               //nolint:mnd
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

// AppEndpoints holds the service endpoint URLs resolved from the catalog API.
// Keys are catalog service IDs (e.g. "chat", "digitize", "similarity").
// Values are maps of endpoint type → URL (e.g. "api" → "https://...").
type AppEndpoints map[string]map[string]string

// ChatBackendURL returns the chat service API (backend) URL.
func (e AppEndpoints) ChatBackendURL() string { return e[catalogSvcChat][endpointTypeAPI] }

// ChatUIURL returns the chat service UI URL.
func (e AppEndpoints) ChatUIURL() string { return e[catalogSvcChat][endpointTypeUI] }

// DigitizeURL returns the digitize service API (backend) URL.
func (e AppEndpoints) DigitizeURL() string { return e[catalogSvcDigitize][endpointTypeAPI] }

// SimilarityURL returns the similarity service API URL.
func (e AppEndpoints) SimilarityURL() string { return e[catalogSvcSimilarity][endpointTypeAPI] }

// CatalogGetApplicationEndpoints fetches endpoint URLs for a named application via the catalog REST API.
func CatalogGetApplicationEndpoints(appName string) (AppEndpoints, error) {
	appClient, err := client.NewApplicationClient(context.Background())
	if err != nil {
		return nil, fmt.Errorf("catalog client init: %w", err)
	}

	list, err := appClient.ListApplications(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}

	for _, app := range list.Data {
		if app.Name == appName {
			return buildAppEndpoints(app.Services), nil
		}
	}

	return nil, fmt.Errorf("application %q not found via catalog API", appName)
}

// buildAppEndpoints converts a service list into an AppEndpoints map.
func buildAppEndpoints(services []types.ApplicationService) AppEndpoints {
	eps := make(AppEndpoints)
	for _, svc := range services {
		svcEps := collectEndpoints(svc.Endpoints)
		if len(svcEps) > 0 {
			eps[svc.CatalogID] = svcEps
		}
	}

	return eps
}

// collectEndpoints extracts type→URL pairs from an endpoint list.
func collectEndpoints(endpoints []map[string]any) map[string]string {
	result := make(map[string]string)
	for _, ep := range endpoints {
		epType, _ := ep["type"].(string)
		epURL, _ := ep["url"].(string)
		if epType != "" && epURL != "" {
			result[epType] = epURL
		}
	}

	return result
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

// ExtractDigitizeURL returns the digitize-backend URL from 'application info' output.
// For podman (catalog path) it matches by "digitize-backend" hostname substring.
// For OpenShift it matches by "digitize-api" hostname substring (the actual route name).
// Falls back to the legacy URL substitution only when neither match is found.
func ExtractDigitizeURL(infoOutput string) string {
	// Podman catalog path: URL contains "digitize-backend".
	if u := ExtractCatalogDigitizeURL(infoOutput); u != "" {
		return u
	}
	// OpenShift path: route hostname contains "digitize-api".
	if u := extractURLBySubstring(infoOutput, "digitize-api"); u != "" {
		return u
	}
	// Legacy fallback: substitute "ui" → "digitize-api" in the first URL found.
	if urls := ExtractURLsFromOutput(infoOutput); len(urls) > 0 {
		return strings.Replace(urls[0], "ui", "digitize-api", 1)
	}

	return ""
}

// ExtractOpenShiftBackendURL extracts the RAG backend URL from OpenShift route output.
// On OpenShift the backend route hostname starts with "backend-".
func ExtractOpenShiftBackendURL(infoOutput string) string {
	return extractURLBySubstring(infoOutput, "backend-")
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

// ExtractCatalogSummarizeURL parses the 'application info' output for the
// summarize service URL.
//
// Actual output line:
//
//	"- Summarize API is available to use at https://summarize-api-<slug>.<domain>."
//
// We match on URL-host substring "summarize-api" which is stable regardless
// of human-readable title changes in info.md.
func ExtractCatalogSummarizeURL(infoOutput string) string {
	return extractURLBySubstring(infoOutput, svcSummarizeAPI)
}

// getOpenShiftRouteURLs fetches OpenShift routes for the given application namespace
// via 'oc get routes' and returns the HTTPS URLs found in the output.
// This is the fallback for OpenShift when 'application info' does not embed URLs.
func getOpenShiftRouteURLs(appName string) []string {
	out, err := common.RunCommand("oc", "get", "routes", "-n", appName,
		"-o", `jsonpath={range .items[*]}{.spec.host}{"\n"}{end}`)
	if err != nil {
		// Fall back to plain text output if jsonpath fails.
		out, err = common.RunCommand("oc", "get", "routes", "-n", appName)
		if err != nil {
			logger.Warningf("[WAIT] oc get routes -n %s failed: %v", appName, err)

			return nil
		}

		return ExtractURLsFromOutput(out)
	}

	var urls []string
	for _, host := range strings.Split(strings.TrimSpace(out), "\n") {
		host = strings.TrimSpace(host)
		if host != "" {
			urls = append(urls, "https://"+host)
		}
	}

	return urls
}

// WaitForApplicationInfoURLs polls 'application info' until service URLs are present.
// For podman requires both chat-bot-backend and similarity-api; for openshift any URL
// suffices — when 'application info' produces no URLs (legacy output format) it falls
// back to 'oc get routes' to resolve them directly from the cluster.
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
			// Podman catalog path: require both chat-bot-backend and similarity-api URLs.
			backendURL, _ := extractCatalogRAGURLs(infoOutput)
			similarityURL := ExtractSimilarityAPIURL(infoOutput)
			if backendURL != "" && similarityURL != "" {
				logger.Infof("[WAIT] application info URLs ready after %d attempt(s) — backend: %s, similarity: %s",
					attempt, backendURL, similarityURL)

				return infoOutput, nil
			}
		} else {
			// OpenShift path: any URL in 'application info' output suffices.
			if len(ExtractURLsFromOutput(infoOutput)) > 0 {
				return infoOutput, nil
			}

			// Legacy OpenShift: 'application info' does not embed URLs — fall back to
			// 'oc get routes' and synthesise a fake info output containing the URLs so
			// callers (GetBaseURL, ExtractDigitizeURL, etc.) can parse them normally.
			ocURLs := getOpenShiftRouteURLs(appName)
			if len(ocURLs) > 0 {
				logger.Infof("[WAIT] OpenShift routes resolved via 'oc get routes' after %d attempt(s): %v",
					attempt, ocURLs)

				return strings.Join(ocURLs, "\n"), nil
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
	const (
		startPollInterval = 15 * time.Second //nolint:mnd
		startMaxRetries   = 30               //nolint:mnd
	)

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

	// Poll until all main pods reach Running state.
	// On slower hardware (ppc64le/Jenkins) pods may still be starting
	// when the CLI returns, so a single immediate check is not sufficient.
	var lastErr error
	for i := 1; i <= startMaxRetries; i++ {
		psOutput, psErr := ApplicationPS(ctx, cfg, appName, appRuntime)
		if psErr != nil {
			return output, psErr
		}

		lastErr = ValidatePodsRunningAfterStart(psOutput, appName, appRuntime)
		if lastErr == nil {
			return output, nil
		}

		logger.Infof("[START] Pods not yet running (attempt %d/%d): %v — retrying in %s",
			i, startMaxRetries, lastErr, startPollInterval)
		time.Sleep(startPollInterval)
	}

	return output, lastErr
}

func deleteAppWithArgs(ctx context.Context, cfg *config.Config, appName string, appRuntime string, errLabel string, args []string) (string, error) {
	output, err := runCLI(ctx, cfg, errLabel, args...)
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

// DeleteApp deletes an application with normal cleanup.
func DeleteApp(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
) (string, error) {
	args := []string{
		"application", "delete", appName,
		"--yes",
		"--runtime", appRuntime,
	}

	return deleteAppWithArgs(ctx, cfg, appName, appRuntime, "application delete", args)
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

	return deleteAppWithArgs(ctx, cfg, appName, appRuntime, "application delete --skip-cleanup", args)
}

// ApplicationInfo runs the 'application info' command.
func ApplicationInfo(ctx context.Context, cfg *config.Config, appName string, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "application info", "application", "info", appName, "--runtime", appRuntime)
}

// ApplicationBackup runs the 'application backup' command.
func ApplicationBackup(ctx context.Context, cfg *config.Config, appName string, target string, filename string, appRuntime string) (string, error) {
	args := []string{
		"application", "backup", appName,
		"--target", target,
		"--runtime", appRuntime,
	}
	if filename != "" {
		args = append(args, "--filename", filename)
	}

	return runCLI(ctx, cfg, "application backup", args...)
}

// ApplicationRestore runs the 'application restore' command.
func ApplicationRestore(ctx context.Context, cfg *config.Config, appName string, target string, filename string, appRuntime string) (string, error) {
	args := []string{
		"application", "restore", appName,
		"--target", target,
		"--runtime", appRuntime,
		"--yes",
	}
	if filename != "" {
		args = append(args, "--filename", filename)
	}

	return runCLI(ctx, cfg, "application restore", args...)
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

// catalogConfigureRunPTY runs 'catalog configure' via PTY with password prompts; shared by all configure variants.
func catalogConfigureRunPTY(ctx context.Context, cfg *config.Config, errLabel string, args []string) (string, error) {
	password := bootstrap.GetCatalogAdminPassword()
	if password == "" {
		return "", fmt.Errorf("CATALOG_PASSWORD environment variable is not set")
	}

	if err := catalogRegistryLogin(); err != nil {
		logger.Warningf("[CLI] registry login warning (non-fatal): %v", err)
	}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	output, err := runWithPTY(ctx, cfg.AIServiceBin, args, password+"\n"+password+"\n")
	if err != nil {
		return output, fmt.Errorf("%s failed: %w\n%s", errLabel, err, output)
	}

	return output, nil
}

// CatalogConfigure runs 'catalog configure' with default settings via PTY.
func CatalogConfigure(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return catalogConfigureRunPTY(ctx, cfg, "catalog configure",
		[]string{"catalog", "configure", "--runtime", appRuntime},
	)
}

// catalogRegistryLogin logs podman into the registry using CI-injected REGISTRY_URL/REGISTRY_USER_NAME/REGISTRY_PASSWORD.
func catalogRegistryLogin() error {
	if url, uname, pswd := bootstrap.GetPodManCreds(); url != "" && uname != "" && pswd != "" {
		logger.Infof("[CLI] Logging podman into registry %s", url)
		if err := bootstrap.PodmanRegistryLogin(url, uname, pswd); err != nil {
			return fmt.Errorf("registry login failed for %s: %w", url, err)
		}
	}

	return nil
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

// extractMarkerURL scans output line-by-line for marker and returns the trimmed text after it.
func extractMarkerURL(output, marker string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, marker); idx >= 0 {
			return strings.TrimRight(strings.TrimSpace(line[idx+len(marker):]), " .,")
		}
	}

	return ""
}

// ExtractCatalogBackendURL extracts the Catalog Backend API URL from 'catalog info' output.
func ExtractCatalogBackendURL(infoOutput string) string {
	return extractMarkerURL(infoOutput, "Catalog Backend API is available at ")
}

// ExtractCatalogBackendURLFromConfigureOutput extracts the Catalog Backend URL from 'catalog configure' output.
func ExtractCatalogBackendURLFromConfigureOutput(configureOutput string) string {
	if u := extractMarkerURL(configureOutput, "Access the Catalog Backend at "); u != "" {
		return u
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

// CatalogApiServerHelp runs 'catalog apiserver --help'; the real apiserver requires a live DB so only help is tested.
func CatalogApiServerHelp(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog apiserver help", "catalog", "apiserver", "--help", "--runtime", appRuntime)
}

// CatalogHashpw runs 'catalog hashpw --stdin' with the provided password piped via stdin.
func CatalogHashpw(ctx context.Context, cfg *config.Config, password, appRuntime string) (string, error) {
	args := []string{"catalog", "hashpw", "--stdin", "--runtime", appRuntime}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	cmd.Stdin = bytes.NewBufferString(password + "\n")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("catalog hashpw failed: %w\n%s", err, output)
	}

	return output, nil
}

// CatalogWhoami runs 'catalog whoami' and returns the combined output.
func CatalogWhoami(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog whoami", "catalog", "whoami", "--runtime", appRuntime)
}

// CatalogLogout runs 'catalog logout' and returns the combined output.
func CatalogLogout(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog logout", "catalog", "logout", "--runtime", appRuntime)
}

// CatalogDbMigrateHelp runs 'catalog dbmigrate --help'; full dbmigrate requires a live DB so only help is tested.
func CatalogDbMigrateHelp(ctx context.Context, cfg *config.Config) (string, error) {
	return runCLI(ctx, cfg, "catalog dbmigrate help", "catalog", "dbmigrate", "--help")
}

// ─────────────────────────────────────────────────────────────────────────────
// Catalog failure runner helpers
//
// These functions are used exclusively by catalog_failure_test.go.  They wrap
// raw exec.CommandContext calls (not runCLI) so that the combined output is
// always returned to the caller even when the command exits non-zero — a
// requirement for failure-test assertions that need to inspect the error text.
// ─────────────────────────────────────────────────────────────────────────────

// CatalogLoginMissingServer invokes `catalog login` without the required
// --server flag.  cobra enforces required flags before RunE is called, so this
// must fail with a "required flag(s) not set" message before touching the
// network or any credentials.
//
// Returns combined stdout+stderr (always populated for flag errors) and the
// exec error.
func CatalogLoginMissingServer(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	args := []string{
		"catalog", "login",
		// --server is intentionally omitted.
		"--username", "admin",
		"--password-stdin",
		"--runtime", appRuntime,
	}

	logger.Infof(
		"[CLI][FAILURE-TEST] Running: %s %s  (--server intentionally omitted)",
		cfg.AIServiceBin, strings.Join(args, " "),
	)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	cmd.Stdin = bytes.NewBufferString("somepassword\n")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("catalog login (missing --server): %w\n%s", err, output)
	}

	return output, nil
}

// CatalogLoginInvalidURL invokes `catalog login` with a --server value whose
// URL scheme is neither http nor https.  validateServerURL() in login.go must
// reject it in PreRunE before any network call is made.
//
// Returns combined stdout+stderr and the exec error.
func CatalogLoginInvalidURL(ctx context.Context, cfg *config.Config, badURL, appRuntime string) (string, error) {
	args := []string{
		"catalog", "login",
		"--server", badURL,
		"--username", "admin",
		"--password-stdin",
		"--runtime", appRuntime,
	}

	logger.Infof(
		"[CLI][FAILURE-TEST] Running: %s %s  (bad URL %q expected to be rejected)",
		cfg.AIServiceBin, strings.Join(args, " "), badURL,
	)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	cmd.Stdin = bytes.NewBufferString("somepassword\n")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("catalog login (invalid URL): %w\n%s", err, output)
	}

	return output, nil
}

// CatalogWhoamiWithoutLogin invokes `catalog whoami` in a subprocess whose
// HOME and XDG_CONFIG_HOME point to an empty temp directory, so no stored
// credentials exist.  client.New() (called inside whoami's RunE) must fail.
//
// homeDir must be an existing, empty directory that the calling test owns.
// Returns combined stdout+stderr and the exec error.
func CatalogWhoamiWithoutLogin(ctx context.Context, cfg *config.Config, homeDir, appRuntime string) (string, error) {
	args := []string{
		"catalog", "whoami",
		"--runtime", appRuntime,
	}

	logger.Infof(
		"[CLI][FAILURE-TEST] Running: %s %s  (HOME=%s — no credentials expected)",
		cfg.AIServiceBin, strings.Join(args, " "), homeDir,
	)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)

	// Redirect the user config directory so no real credentials are found.
	// os.UserConfigDir() reads $HOME (Linux/macOS) and $XDG_CONFIG_HOME.
	cmd.Env = filteredProcessEnv("HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME")
	cmd.Env = append(cmd.Env,
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+homeDir,
	)

	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("catalog whoami (no credentials): %w\n%s", err, output)
	}

	return output, nil
}

// CatalogConfigureUnpairedSSL invokes `catalog configure` with --ssl-cert but
// without --ssl-key.  checkSSLFlagsPaired() in configure.go must reject it in
// PreRunE before any state is modified.
//
// certPath can be any non-empty string — the flag validator only checks that
// both flags are provided together; it does not read the file at this point.
// Returns combined stdout+stderr and the exec error.
func CatalogConfigureUnpairedSSL(ctx context.Context, cfg *config.Config, certPath, appRuntime string) (string, error) {
	args := []string{
		"catalog", "configure",
		"--runtime", appRuntime,
		"--ssl-cert", certPath,
		// --ssl-key intentionally omitted to trigger the pairing check.
	}

	logger.Infof(
		"[CLI][FAILURE-TEST] Running: %s %s  (--ssl-key omitted, pairing check expected)",
		cfg.AIServiceBin, strings.Join(args, " "),
	)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("catalog configure (unpaired SSL): %w\n%s", err, output)
	}

	return output, nil
}

// CatalogConfigureInvalidPort invokes `catalog configure` with an --https-port
// value outside the valid 1–65535 range.  validateConfigureFlags() in
// configure.go must reject it in PreRunE before any deployment action runs.
//
// Returns combined stdout+stderr and the exec error.
func CatalogConfigureInvalidPort(ctx context.Context, cfg *config.Config, port int, appRuntime string) (string, error) {
	args := []string{
		"catalog", "configure",
		"--runtime", appRuntime,
		"--https-port", fmt.Sprintf("%d", port),
	}

	logger.Infof(
		"[CLI][FAILURE-TEST] Running: %s %s  (port %d expected to be rejected)",
		cfg.AIServiceBin, strings.Join(args, " "), port,
	)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("catalog configure (invalid port %d): %w\n%s", port, err, output)
	}

	return output, nil
}

// filteredProcessEnv returns the current process environment (os.Environ) with
// the named keys removed.  Used to strip HOME / XDG_CONFIG_HOME before
// injecting test-isolated values into a subprocess.
func filteredProcessEnv(excludeKeys ...string) []string {
	exclude := make(map[string]struct{}, len(excludeKeys))
	for _, k := range excludeKeys {
		exclude[k] = struct{}{}
	}

	all := os.Environ()
	filtered := make([]string, 0, len(all))

	for _, pair := range all {
		key := pair
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			key = pair[:idx]
		}

		if _, skip := exclude[key]; !skip {
			filtered = append(filtered, pair)
		}
	}

	return filtered
}

// ─────────────────────────────────────────────────────────────────────────────
// Catalog configure runner helpers
// ─────────────────────────────────────────────────────────────────────────────

// CatalogConfigureWithBasedir runs 'catalog configure --basedir <path>' via PTY.
func CatalogConfigureWithBasedir(ctx context.Context, cfg *config.Config, basedir string, appRuntime string) (string, error) {
	return catalogConfigureRunPTY(ctx, cfg, "catalog configure --basedir",
		[]string{"catalog", "configure", "--basedir", basedir, "--runtime", appRuntime},
	)
}

// CatalogConfigureWithSSL runs 'catalog configure --ssl-cert <cert> --ssl-key <key>' via PTY.
func CatalogConfigureWithSSL(ctx context.Context, cfg *config.Config, certPath, keyPath string, appRuntime string) (string, error) {
	return catalogConfigureRunPTY(ctx, cfg, "catalog configure --ssl-cert/--ssl-key",
		[]string{"catalog", "configure", "--ssl-cert", certPath, "--ssl-key", keyPath, "--runtime", appRuntime},
	)
}

// CatalogConfigureResetCert runs 'catalog configure --reset-certificate --ssl-cert <cert> --ssl-key <key>'.
func CatalogConfigureResetCert(ctx context.Context, cfg *config.Config, certPath, keyPath string, appRuntime string) (string, error) {
	return runCLI(ctx, cfg, "catalog configure --reset-certificate",
		"catalog", "configure", "--reset-certificate",
		"--ssl-cert", certPath, "--ssl-key", keyPath, "--runtime", appRuntime,
	)
}

// CatalogConfigureResetAuth runs 'catalog configure --reset-podman-auth'.
// The command presents an interactive huh.NewConfirm() prompt; a PTY with "y\n"
// is required to auto-confirm it — plain exec.Cmd with no PTY would hang until
// the context deadline kills the process.
func CatalogConfigureResetAuth(ctx context.Context, cfg *config.Config, appRuntime string) (string, error) {
	args := []string{"catalog", "configure", "--reset-podman-auth", "--runtime", appRuntime}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	output, err := runWithPTY(ctx, cfg.AIServiceBin, args, "y\n")
	if err != nil {
		return output, fmt.Errorf("catalog configure --reset-podman-auth failed: %w\n%s", err, output)
	}

	return output, nil
}

// CatalogConfigureWithArgs runs 'catalog configure' with arbitrary extra args for negative / flag-combo tests.
func CatalogConfigureWithArgs(ctx context.Context, cfg *config.Config, appRuntime string, extraArgs ...string) (string, error) {
	args := append([]string{"catalog", "configure", "--runtime", appRuntime}, extraArgs...)
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	return output, err
}
