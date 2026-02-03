package power

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type PowerRule struct{}

func NewPowerRule() *PowerRule {
	return &PowerRule{}
}

func (r *PowerRule) Name() string {
	return "power"
}

func (r *PowerRule) Description() string {
	return "Validates that the system is running on IBM Power11 (ppc64le)"
}

func (r *PowerRule) Verify() error {
	logger.Infoln("Validating IBM Power version...", logger.VerbosityLevelDebug)

	if runtime.GOARCH != "ppc64le" {
		return fmt.Errorf("unsupported architecture: %s. IBM Power architecture (ppc64le) is required", runtime.GOARCH)
	}

	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil && strings.Contains(strings.ToLower(string(data)), "power11") {
		return nil
	}

	return fmt.Errorf("unsupported IBM Power version: Power11 is required")
}

func (r *PowerRule) Message() string {
	return "System is running on IBM Power11 (ppc64le)"
}

func (r *PowerRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *PowerRule) Hint() string {
	return "This tools requires IBM Power11 (ppc64le)"
}
