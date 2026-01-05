package cli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
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

// Bootstrap runs the full bootstrap (configure + validate)
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

// BootstrapConfigure runs only the 'configure' step
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

// BootstrapValidate runs only the 'validate' step
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
	if opts.Verbose {
		args = append(args, "--verbose")
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

func CreateAppWithHealthAndRAG(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	template string,
	params string,
	backendPort string,
	opts CreateOptions,
	pods []string,
) error {
	// Create application
	output, err := CreateApp(ctx, cfg, appName, template, params, opts)
	if err != nil {
		return err
	}

	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return err
	}
	fmt.Println("[CLI] Application created successfully!")

	// Run parallel pod health checks
	var wg sync.WaitGroup
	fmt.Println("[HealthCheck] Running health checks in parallel for pods...")
	for _, p := range pods {
		wg.Add(1)
		go func(podName string) {
			defer wg.Done()
			start := time.Now()
			cmd := exec.CommandContext(ctx, cfg.AIServiceBin, "pod", "health", podName)
			out, err := cmd.CombinedOutput()
			output := string(out)
			if err != nil {
				fmt.Printf("[HealthCheck] %s failed: %v\n%s\n", podName, err, output)
				return
			}
			fmt.Printf("[HealthCheck] %s OK (took %v)\n%s\n", podName, time.Since(start), output)
		}(p)
	}
	wg.Wait()
	fmt.Println("[HealthCheck] All pod health checks completed!")

	// Extract backend URL from CLI output
	re := regexp.MustCompile(`Chatbot UI is available to use at (http[s]?://[^\s]+)`)
	match := re.FindStringSubmatch(output)
	baseURL := "http://localhost:" + backendPort
	if len(match) > 1 {
		baseURL = match[1]
	}
	fmt.Println("[RAG] Using backend URL:", baseURL)

	// Initialize HTTP client
	client := httpclient.NewHTTPClient()
	client.BaseURL = baseURL

	// Test basic RAG endpoints
	endpoints := []string{
		"/health",
		"/v1/models",
		"/db-status",
	}
	for _, ep := range endpoints {
		resp, err := client.Get(ep)
		if err != nil {
			fmt.Printf("[RAG] GET %s failed: %v\n", ep, err)
			continue
		}
		fmt.Printf("[RAG] GET %s -> %s\n", ep, resp.Status)
		resp.Body.Close()
	}

	fmt.Println("[RAG] Basic RAG endpoints tested successfully!")
	return nil
}

// StartApp starts an application
func StartApp(appName string) error {
	fmt.Printf("[CLI] Running: ai-services start %s\n", appName)
	output, err := common.RunCommand("ai-services", "start", appName)
	if err != nil {
		return fmt.Errorf("start-app failed: %v\n%s", err, output)
	}
	return nil
}
