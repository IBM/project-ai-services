package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
	"github.com/project-ai-services/ai-services/tests/e2e/httpclient"
)

type CreateOptions struct {
	SkipImageDownload bool
	SkipModelDownload bool
	SkipValidation    string
	Verbose           bool
}

// Bootstrap runs the full bootstrap (configure + validate).
func Bootstrap(ctx context.Context) (string, error) {
	binPath, err := bootstrap.BuildOrVerifyCLIBinary(ctx)
	if err != nil {
		return "", err
	}
	fmt.Println("[CLI] Running:", binPath, "bootstrap")
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
	fmt.Println("[CLI] Running:", binPath, "bootstrap configure")
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
	fmt.Println("[CLI] Running:", binPath, "bootstrap validate")
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
	fmt.Printf("[CLI] Running: %s %s\n", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("application create failed: %w\n%s", err, output)
	}

	return output, nil
}

// CreateAppWithHealthAndRAG creates an application, waits for health checks, and validates RAG endpoints.
func CreateAppWithHealthAndRAG(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	template string,
	params string,
	backendPort string,
	uiPort string,
	opts CreateOptions,
	_ []string,
) error {
	output, err := CreateApp(ctx, cfg, appName, template, params, opts)
	if err != nil {
		return err
	}
	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return err
	}
	hostIP, err := extractHostIP(output)
	if err != nil {
		return err
	}
	backendURL := fmt.Sprintf("http://%s:%s", hostIP, backendPort)
	client := httpclient.NewHTTPClient()
	client.BaseURL = backendURL
	endpoints := []string{
		"/health",
		"/v1/models",
		"/db-status",
	}
	const (
		maxRetries = 10
		waitTime   = 15 * time.Second
	)
	for _, ep := range endpoints {
		if err := waitForEndpointOK(client, ep, maxRetries, waitTime); err != nil {
			return err
		}
	}
	uiURL := fmt.Sprintf("http://%s:%s", hostIP, uiPort)
	fmt.Println("[UI] Chatbot UI available at:", uiURL)

	return nil
}

// waitForEndpointOK polls the given endpoint until it returns HTTP 200 OK or exhausts retries.
func waitForEndpointOK(
	client *httpclient.HTTPClient,
	endpoint string,
	maxRetries int,
	waitTime time.Duration,
) error {
	var lastErr error
	for i := 1; i <= maxRetries; i++ {
		resp, err := client.Get(endpoint)
		if err == nil && resp.StatusCode == http.StatusOK {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body for %s: %v\n", endpoint, cerr)
			}
			fmt.Printf("[RAG] GET %s -> 200 OK\n", endpoint)

			return nil
		}
		if resp != nil {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body for %s: %v\n", endpoint, cerr)
			}
		}
		lastErr = err
		fmt.Printf(
			"[RAG] Waiting for %s (attempt %d/%d)\n",
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

// HelpCommand runs the 'help' command with or without arguments.
func HelpCommand(ctx context.Context, cfg *config.Config, args []string) (string, error) {
	fmt.Printf("[CLI] Running: %s %s\n", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return output, fmt.Errorf("help command run failed: %w\n%s", err, output)
	}

	return output, nil
}

// ApplicationPS runs the 'application ps' command to list application pods.
func ApplicationPS(ctx context.Context, cfg *config.Config, appName string) (string, error) {
	args := []string{"application", "ps"}
	if appName != "" {
		args = append(args, appName)
	}
	fmt.Printf("[CLI] Running: %s %s\n", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	fmt.Println(output)

	if err != nil {
		return output, fmt.Errorf("application ps failed: %w\n%s", err, output)
	}

	return output, nil
}

// StopApp stops an application.
func StopApp(ctx context.Context, cfg *config.Config, appName string) (string, error) {
	args := []string{"application", "stop", appName, "--yes"}

	fmt.Printf("[CLI] Running: %s %s\n", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	fmt.Println(output)

	if err != nil {
		return output, fmt.Errorf("application stop failed: %w\n%s", err, output)
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

// DeleteApp deletes an application.
func DeleteApp(ctx context.Context, cfg *config.Config, appName string) (string, error) {
	args := []string{"application", "delete", appName, "--yes"}
	fmt.Printf("[CLI] Running: %s %s\n", cfg.AIServiceBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	fmt.Println(output)
	if err != nil {
		return output, fmt.Errorf("application delete failed: %w\n%s", err, output)
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
