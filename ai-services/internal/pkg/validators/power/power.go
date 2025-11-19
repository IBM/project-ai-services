package power

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type PowerRule struct{}

func NewPowerRule() *PowerRule {
	return &PowerRule{}
}

func (r *PowerRule) Name() string {
	return "power"
}

func (r *PowerRule) Verify() error {
	logger.Infoln("Validating IBM Power version...", 2)

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
