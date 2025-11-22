package spyre

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"k8s.io/klog/v2"
)

type SpyreRule struct{}

func NewSpyreRule() *SpyreRule {
	return &SpyreRule{}
}

func (r *SpyreRule) Name() string {
	return "spyre"
}

func (r *SpyreRule) Verify() error {
	klog.V(2).Infoln("Validating Spyre attachment...")
	out, err := exec.Command("lspci").Output()
	if err != nil {
		return fmt.Errorf("failed to execute lspci command: %w", err)
	}

	if !strings.Contains(string(out), "IBM Spyre Accelerator") {
		return fmt.Errorf("IBM Spyre Accelerator is not attached to the LPAR")
	}

	return nil
}

func (r *SpyreRule) Message() string {
	return "IBM Spyre Accelerator is attached to the LPAR"
}

func (r *SpyreRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *SpyreRule) Hint() string {
	return "IBM Spyre Accelerator hardware is required but not detected."
}
