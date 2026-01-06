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

// CreateApp creates an application via the CLI
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

// CreateAPPWithHealthAndRAG creates an application and validates its health and RAG endpoints
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

	// Create application
	output, err := CreateApp(ctx, cfg, appName, template, params, opts)
	if err != nil {
		return err
	}
	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return err
	}
	fmt.Println("[CLI] Application created successfully!")

	// Resolve HOST IP from deployment output
	hostIP, err := extractHostIP(output)
	if err != nil {
		return err
	}

	//  Backend base URL (ALWAYS backend.port)
	backendURL := fmt.Sprintf("http://%s:%s", hostIP, backendPort)
	fmt.Println("[RAG] Using backend URL:", backendURL)

	// HTTP client with timeout
	client := httpclient.NewHTTPClient()
	client.BaseURL = backendURL

	// Endpoints to validate (exact API spec)
	endpoints := []string{
		"/health",
		"/v1/models",
		"/db-status",
	}

	// Retry loop (startup delay safe)
	const (
		maxRetries = 10
		waitTime   = 15 * time.Second
	)
	for _, ep := range endpoints {
		var lastErr error
		for i := 1; i <= maxRetries; i++ {
			resp, err := client.Get(ep)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				fmt.Printf("[RAG] GET %s -> 200 OK\n", ep)
				lastErr = nil
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			lastErr = err
			fmt.Printf("[RAG] Waiting for %s (attempt %d/%d)\n", ep, i, maxRetries)
			time.Sleep(waitTime)
		}
		if lastErr != nil {
			return fmt.Errorf("[RAG] endpoint %s failed after retries: %w", ep, lastErr)
		}
	}
	fmt.Println("[RAG] Backend health & RAG APIs validated successfully")
	uiURL := fmt.Sprintf("http://%s:%s", hostIP, uiPort)
	fmt.Println("--------------------------------------------------")
	fmt.Println("[UI] Chatbot UI is available at:", uiURL)
	fmt.Println("--------------------------------------------------")
	return nil
}

func extractHostIP(output string) (string, error) {
	re := regexp.MustCompile(`http[s]?://([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	match := re.FindStringSubmatch(output)
	if len(match) < 2 {
		return "", fmt.Errorf("unable to determine application host IP from CLI output")
	}
	return match[1], nil
}
