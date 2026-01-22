package rhaiis

import (
	"fmt"
	"os/exec"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const rhaiisImageRef = "registry.redhat.io/rhaiis/vllm-spyre-rhel9:3"

// RHAIISRule validates Red Hat AI Inference Server license.
type RHAIISRule struct{}

// NewRHAIISRule creates a new RH-AIIS license validator.
func NewRHAIISRule() *RHAIISRule {
	return &RHAIISRule{}
}

// Name returns the identifier for this validator.
func (r *RHAIISRule) Name() string {
	return "rhaiis"
}

// Verify checks if the RHAIIS license is available by inspecting the vllm-spyre image manifest.
// Runs `podman manifest inspect registry.redhat.io/rhaiis/vllm-spyre-rhel9:3`; if the command
// returns an error, the license is not available.
func (r *RHAIISRule) Verify() error {
	logger.Infoln("Validating Red Hat AI Inference Server license...", logger.VerbosityLevelDebug)

	if _, err := exec.LookPath("podman"); err != nil {
		return fmt.Errorf("podman not found: %w", err)
	}

	cmd := exec.Command("podman", "manifest", "inspect", rhaiisImageRef)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("RHAIIS license not available")
	}

	return nil
}

// Message returns the success message when validation passes.
func (r *RHAIISRule) Message() string {
	return "RHAIIS license is available"
}

// Level returns the validation level (error or warning).
func (r *RHAIISRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

// Hint returns helpful information when validation fails.
func (r *RHAIISRule) Hint() string {
	return "Ensure your system has a valid Red Hat AI Inference Server subscription and that you are logged in to registry.redhat.io. " +
		"Try: podman login registry.redhat.io"
}
