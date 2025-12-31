package cli

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

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

// CreateApp creates a new application
func CreateApp(appName string) error {
	fmt.Printf("[CLI] Running: ai-services create-app %s\n", appName)
	output, err := common.RunCommand("ai-services", "create-app", appName)
	if err != nil {
		return fmt.Errorf("create-app failed: %v\n%s", err, output)
	}
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
