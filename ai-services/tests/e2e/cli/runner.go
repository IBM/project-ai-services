package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

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
	_ []string, // kept for compatibility, unused
) error {

	// Create app
	output, err := CreateApp(ctx, cfg, appName, template, params, opts)
	if err != nil {
		return err
	}
	if err := ValidateCreateAppOutput(output, appName); err != nil {
		return err
	}
	fmt.Println("[CLI] Application created successfully!")
	// Resolve backend URL (ALWAYS backend port)
	backendURL := "http://localhost:" + backendPort
	re := regexp.MustCompile(`(http[s]?://[0-9.:]+)`)

	if match := re.FindStringSubmatch(output); len(match) > 1 {
		backendURL = strings.TrimRight(match[1], ".")
	}
	fmt.Println("[RAG] Using backend URL:", backendURL)

	client := httpclient.NewHTTPClient()
	client.BaseURL = backendURL
	// Health + RAG endpoints (STRICT)
	endpoints := []string{
		"/health",
		"/v1/models",
		"/db-status",
	}
	for _, ep := range endpoints {
		resp, err := client.Get(ep)
		if err != nil {
			return fmt.Errorf("[RAG] GET %s failed: %w", ep, err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("[RAG] GET %s returned %s", ep, resp.Status)
		}
		resp.Body.Close()
		fmt.Printf("[RAG] GET %s -> %s\n", ep, resp.Status)
	}
	fmt.Println("[RAG] Backend health & RAG endpoints validated")
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
