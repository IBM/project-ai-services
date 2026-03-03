package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type PlatformRule struct{}

func NewPlatformRule() *PlatformRule {
	return &PlatformRule{}
}

func (r *PlatformRule) Name() string {
	return "rhel"
}

func (r *PlatformRule) Description() string {
	return "Validates that the operating system is RHEL version 9.6 or higher."
}

func (r *PlatformRule) Verify() error {
	logger.Infoln("Validating operating system...", logger.VerbosityLevelDebug)

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}

	// verify if OS is RHEL
	osInfo := string(data)
	isRHEL := strings.Contains(osInfo, "Red Hat Enterprise Linux") ||
		strings.Contains(osInfo, `ID="rhel"`) ||
		strings.Contains(osInfo, `ID=rhel`)

	if !isRHEL {
		return fmt.Errorf("unsupported operating system: only RHEL is supported")
	}

	// fetch rhel version
	version, err := fetchRhelVersion(osInfo)
	if err != nil {
		return err
	}

	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}

	// verify if version is 9.6 or higher
	if major < 9 || (major == 9 && minor < 6) {
		return fmt.Errorf("unsupported RHEL version: %s. Minimum required version is 9.6", version)
	}

	return nil
}

// fetchRhelVersion -> fetches the Rhel version from /etc/os-release.
func fetchRhelVersion(osInfo string) (string, error) {
	idx := strings.Index(osInfo, "VERSION_ID=")
	if idx == -1 {
		return "", fmt.Errorf("unable to determine OS version")
	}

	rest := osInfo[idx+len("VERSION_ID="):]
	if end := strings.IndexByte(rest, '\n'); end != -1 {
		rest = rest[:end]
	}

	version := strings.Trim(rest, `"`)

	return version, nil
}

func (r *PlatformRule) Message() string {
	return "The LPAR is running a supported version of the operating system (RHEL 9.6 or higher)."
}

func (r *PlatformRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *PlatformRule) Hint() string {
	return "This tool requires RHEL version 9.6, please install or upgrade to a supported platform"
}
