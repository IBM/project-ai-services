package platform

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	rocmMinRHELMajor = 10
	rocmMinRHELMinor = 3
)

// ROCmPlatformRule validates that the operating system is RHEL 10.3 or higher, the release the
// ROCm stack and the vLLM ROCm image are built for.
type ROCmPlatformRule struct{}

func NewROCmPlatformRule() *ROCmPlatformRule {
	return &ROCmPlatformRule{}
}

func (r *ROCmPlatformRule) Name() string {
	return "rhel-rocm"
}

func (r *ROCmPlatformRule) Description() string {
	return "Validates that the operating system is RHEL version 10.3 or higher, as required for ROCm."
}

func (r *ROCmPlatformRule) Verify(_ context.Context) error {
	logger.Debugln("Validating operating system for ROCm...")

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}

	return checkROCmRHELVersion(string(data))
}

// checkROCmRHELVersion verifies that the os-release contents describe RHEL 10.3 or higher.
func checkROCmRHELVersion(osInfo string) error {
	isRHEL := strings.Contains(osInfo, "Red Hat Enterprise Linux") ||
		strings.Contains(osInfo, `ID="rhel"`) ||
		strings.Contains(osInfo, `ID=rhel`)
	if !isRHEL {
		return fmt.Errorf("unsupported operating system: only RHEL is supported")
	}

	version, err := fetchRhelVersion(osInfo)
	if err != nil {
		return err
	}

	major, minor, err := parseMajorMinor(version)
	if err != nil {
		return err
	}

	if major < rocmMinRHELMajor || (major == rocmMinRHELMajor && minor < rocmMinRHELMinor) {
		return fmt.Errorf("unsupported RHEL version: %s. Minimum required version for ROCm is %d.%d",
			version, rocmMinRHELMajor, rocmMinRHELMinor)
	}

	return nil
}

// parseMajorMinor parses a "<major>[.<minor>]" version string. A missing minor is treated as 0.
func parseMajorMinor(version string) (major, minor int, err error) {
	parts := strings.Split(version, ".")

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("unable to parse OS version %q: %w", version, err)
	}

	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("unable to parse OS version %q: %w", version, err)
		}
	}

	return major, minor, nil
}

func (r *ROCmPlatformRule) Message() string {
	return "The LPAR is running a supported version of the operating system for ROCm (RHEL 10.3 or higher)."
}

func (r *ROCmPlatformRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *ROCmPlatformRule) Hint() string {
	return "ROCm requires RHEL version 10.3 or higher, please install or upgrade to a supported platform"
}
