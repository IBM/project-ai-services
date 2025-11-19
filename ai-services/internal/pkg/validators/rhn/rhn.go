package rhn

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type RHNRule struct{}

func NewRHNRule() *RHNRule {
	return &RHNRule{}
}

func (r *RHNRule) Name() string {
	return "rhn"
}

func (r *RHNRule) Verify() error {
	logger.Infoln("Validating RHN registration...", 2)
	cmd := exec.Command("dnf", "repolist")
	output, err := cmd.CombinedOutput()

	// Checking the output content first, as dnf may return non-zero exit code
	// even when the system is registered
	outputStr := string(output)
	if strings.Contains(outputStr, "This system is not registered") {
		return fmt.Errorf("system is not registered with RHN")
	}

	if err != nil {
		return fmt.Errorf("failed to check registration status: %w", err)
	}

	return nil
}

func (r *RHNRule) Message() string {
	return "System is registered with RHN"
}
