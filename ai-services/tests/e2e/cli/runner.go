package cli

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

func Bootstrap(ctx context.Context) (string, error) {
	binPath, err := bootstrap.BuildOrVerifyCLIBinary(ctx)
	if err != nil {
		return "", err
	}
	//Running ai-services bootstrap
	fmt.Println("[CLI] Running:", binPath, "bootstrap")
	output, err := common.RunCommand(binPath, "bootstrap")
	if err != nil {
		return output, err
	}
	return output, nil
}

// CreateApp creates a new application
func CreateApp(appName string) error {
	fmt.Printf("[CLI] Running: ai-services create-app %s\n", appName)
	// Placeholder implementation
	return nil
}

// StartApp starts an application
func StartApp(appName string) error {
	fmt.Printf("[CLI] Running: ai-services start %s\n", appName)
	// Placeholder implementation
	return nil
}
