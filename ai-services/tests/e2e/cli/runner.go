package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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

// Bootstrap runs the full bootstrap (configure + validate).
func Bootstrap(ctx context.Context) (string, error) {
	binPath, err := bootstrap.BuildOrVerifyCLIBinary(ctx)
	if err != nil {
		return "", err
	}
	logger.Infof("[CLI] Running: %s bootstrap", binPath)
	output, err := common.RunCommand(binPath, "bootstrap")
	if err != nil {
		return output, err
	}

	return output, nil
}

// BootstrapConfigure runs only the 'configure' step.
func BootstrapConfigure(ctx context.Context) (string, error) {
	binPath, err := bootstrap.BuildOrVerifyCLIBinary(ctx)
	if err != nil {
		return "", err
	}
	logger.Infof("[CLI] Running: %s bootstrap configure", binPath)
	output, err := common.RunCommand(binPath, "bootstrap", "configure")
	if err != nil {
		return output, err
	}

	return output, nil
}

// BootstrapValidate runs only the 'validate' step.
func BootstrapValidate(ctx context.Context) (string, error) {
	binPath, err := bootstrap.BuildOrVerifyCLIBinary(ctx)
	if err != nil {
		return "", err
	}
	logger.Infof("[CLI] Running: %s bootstrap validate", binPath)
	output, err := common.RunCommand(binPath, "bootstrap", "validate")
	if err != nil {
		return output, err
	}

	return output, nil
}

// CreateApp creates an application via the CLI.
func CreateApp(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	template string,
	params string,
	opts CreateOptions,
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
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application create failed: %w\n%s", err, output)
	}

	return output, nil
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
) (string, error) {
	const (
		maxRetries            = 10
		waitTime              = 15 * time.Second
		defaultCommandTimeout = 10 * time.Second
	)
	output, err := CreateApp(ctx, cfg, appName, template, params, opts)
	if err != nil {
		return output, err
	}
	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return output, err
	}
	hostIP, err := extractHostIP(output)
	if err != nil {
		return output, err
	}
	backendURL := fmt.Sprintf("http://%s:%s", hostIP, backendPort)
	httpClient := &http.Client{
		Timeout: defaultCommandTimeout,
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
	uiURL := fmt.Sprintf("http://%s:%s", hostIP, uiPort)
	logger.Infof("[UI] Chatbot UI available at: %s", uiURL)

	return output, nil
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

// extractHostIP extracts the host IP from the CLI output using regex.
func extractHostIP(output string) (string, error) {
	const minMatchGroups = 2
	re := regexp.MustCompile(`http[s]?://([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	match := re.FindStringSubmatch(output)
	if len(match) < minMatchGroups {
		return "", fmt.Errorf("unable to determine application host IP from CLI output")
	}

	return match[1], nil
}

// GetBaseURL constructs the base URL from the CLI output and backend port.
func GetBaseURL(createOutput string, backendPort string) (string, error) {
	hostIP, err := extractHostIP(createOutput)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("http://%s:%s", hostIP, backendPort), nil
}

// HelpCommand runs the 'help' command with or without arguments.
func HelpCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("help command run failed: %w\n%s", err, output)
	}

	return output, nil
}

// ApplicationPS runs the 'application ps' command to list application pods.
func ApplicationPS(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	flags ...string,
) (string, error) {
	args := []string{"application", "ps"}

	if appName != "" {
		args = append(args, appName)
	}

	args = append(args, flags...)

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application ps failed: %w\n%s", err, output)
	}

	return output, nil
}

// ListImage from the given application template.
func ListImage(ctx context.Context, cfg *config.Config, templateName string) error {
	args := []string{"application", "image", "list", "--template", templateName}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return fmt.Errorf("list images failed: %w\n%s", err, output)
	}
	if err := ValidateImageListOutput(output); err != nil {
		return err
	}

	return nil
}

// PullImage from the given application template.
func PullImage(ctx context.Context, cfg *config.Config, templateName string) error {
	//perform ICR login
	url, uname, pswd := bootstrap.GetPodManCreds()
	loginErr := bootstrap.PodmanRegistryLogin(url, uname, pswd)
	if loginErr != nil {
		return fmt.Errorf("pull images failed due to podman login err: %w", loginErr)
	}

	//perform RH registry login
	url, uname, pswd = bootstrap.GetRHRegistryCreds()
	loginErr = bootstrap.PodmanRegistryLogin(url, uname, pswd)
	if loginErr != nil {
		return fmt.Errorf("pull images failed due to podman login err: %w", loginErr)
	}

	args := []string{"application", "image", "pull", "--template", templateName}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return fmt.Errorf("pull images failed: %w\n%s", err, output)
	}
	if err := ValidatePullImageOutput(output, templateName); err != nil {
		return err
	}

	return nil
}

// StopAppWithPods stops an application specifying pods to stop.
func StopAppWithPods(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	pods []string,
) (string, error) {
	podArg := strings.Join(pods, ",")
	args := []string{
		"application", "stop", appName,
		"--pod", podArg,
		"--yes",
	}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)

	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("application stop --pod failed: %w\n%s", err, output)
	}

	if err := ValidateStopAppOutput(output); err != nil {
		return output, err
	}

	psOutput, err := ApplicationPS(ctx, cfg, appName)
	if err != nil {
		return output, err
	}

	if err := ValidatePodsExitedAfterStop(psOutput, appName); err != nil {
		return output, err
	}

	return output, nil
}

func StartApplication(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	opts StartOptions,
) (string, error) {
	args := []string{"application", "start", appName, "--yes"}

	if opts.Pod != "" {
		args = append(args, "--pod="+opts.Pod)
	}
	if opts.SkipLogs {
		args = append(args, "--skip-logs")
	}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	logger.Infof("[CLI] Output: %s", output)

	if err != nil {
		return output, fmt.Errorf("application start failed: %w\n%s", err, output)
	}

	// Validate output.
	if err := ValidateStartAppOutput(output); err != nil {
		return output, err
	}

	// Verify pods are running again.
	psOutput, err := ApplicationPS(ctx, cfg, appName)
	if err != nil {
		return output, err
	}

	if err := ValidatePodsRunningAfterStart(psOutput, appName); err != nil {
		return output, err
	}

	return output, nil
}

// DeleteAppSkipCleanup deletes an application with --skip-cleanup flag.
func DeleteAppSkipCleanup(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) (string, error) {
	args := []string{
		"application", "delete", appName,
		"--skip-cleanup",
		"--yes",
	}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)

	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application delete --skip-cleanup failed: %w\n%s", err, output)
	}

	if err := ValidateDeleteAppOutput(output, appName); err != nil {
		return output, err
	}

	psOutput, err := ApplicationPS(ctx, cfg, appName)
	if err != nil {
		return output, err
	}
	if err := ValidateNoPodsAfterDelete(psOutput); err != nil {
		return output, err
	}

	return output, nil
}

// ApplicationInfo runs the 'application info' command.
func ApplicationInfo(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) (string, error) {
	args := []string{"application", "info", appName}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)

	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application info failed: %w\n%s", err, output)
	}

	return output, nil
}

// ModelList lists models for a given application template.
func ModelList(ctx context.Context, cfg *config.Config, templateName string) (string, error) {
	args := []string{"application", "model", "list", "--template", templateName}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("application model list failed: %w\n%s", err, output)
	}

	return output, nil
}

// ModelDownload downloads a model for a given application template.
func ModelDownload(ctx context.Context, cfg *config.Config, templateName string) (string, error) {
	args := []string{"application", "model", "download", "--template", templateName}
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("application model download failed: %w\n%s", err, output)
	}

	return output, nil
}

// TemplatesCommand runs the 'application template' command.
func TemplatesCommand(ctx context.Context, cfg *config.Config) (string, error) {
	logger.Infof("[CLI] Running: %s application templates", cfg.AIServiceBin)
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, "application", "templates")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application templates command run failed: %w\n%s", err, output)
	}

	return output, nil
}

// VersionCommand runs the 'version' command.
func VersionCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("version command run failed: %w\n%s", err, output)
	}

	return output, nil
}

// GitVersionCommands runs the git commands required for version check.
func GitVersionCommands(ctx context.Context) (string, string, error) {
	versionCmd := "describe --tags --always"
	commitCmd := "rev-parse --short HEAD"

	logger.Infof("[CLI] Running: git %s", strings.Split(versionCmd, " "))
	vcmd := exec.CommandContext(ctx, "git", strings.Split(versionCmd, " ")...)
	vout, err := vcmd.CombinedOutput()
	voutput := string(vout)
	if err != nil {
		return voutput, "", fmt.Errorf("git version command run failed: %w\n%s", err, voutput)
	}

	logger.Infof("[CLI] Running: git %s", strings.Split(commitCmd, " "))
	ccmd := exec.CommandContext(ctx, "git", strings.Split(commitCmd, " ")...)
	cout, err := ccmd.CombinedOutput()
	coutput := string(cout)
	if err != nil {
		return voutput, coutput, fmt.Errorf("git commit command run failed: %w\n%s", err, coutput)
	}

	return voutput, coutput, nil
}
