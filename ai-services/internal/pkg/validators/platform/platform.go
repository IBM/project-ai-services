package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type PlatformRule struct{}

func NewPlatformRule() *PlatformRule {
	return &PlatformRule{}
}

func (r *PlatformRule) Name() string {
	return "platform"
}

func (r *PlatformRule) Verify() error {
	logger.Infoln("Validating operating system...", 2)

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

	// verify if version is 9.6 or higher
	idx := strings.Index(osInfo, "VERSION_ID=")
	if idx == -1 {
		return fmt.Errorf("unable to determine OS version")
	}
	rest := osInfo[idx+len("VERSION_ID="):]
	if end := strings.IndexByte(rest, '\n'); end != -1 {
		rest = rest[:end]
	}
	version := strings.Trim(rest, `"`)

	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}

	if major < 9 || (major == 9 && minor < 6) {
		return fmt.Errorf("unsupported RHEL version: %s. Minimum required version is 9.6", version)
	}

	return nil

}

func (r *PlatformRule) Message() string {
	return "Operating system is RHEL with version 9.6"
}
