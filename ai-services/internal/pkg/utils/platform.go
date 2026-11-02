package utils

import (
	"fmt"
	"os"
	"runtime"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// SkipPlatformCheckEnv, when set to "true", bypasses the podman platform check.
// Intended for local development only (e.g. running the catalog from macOS via Tilt).
const SkipPlatformCheckEnv = "AI_SERVICES_SKIP_PLATFORM_CHECK"

// CheckPodmanPlatformSupport checks if the current platform supports podman runtime.
// Podman runtime is only supported on linux/ppc64le, unless SkipPlatformCheckEnv is "true".
func CheckPodmanPlatformSupport(runtimeType types.RuntimeType) error {
	// Only check if podman runtime is being used
	if runtimeType != types.RuntimeTypePodman {
		return nil
	}

	if os.Getenv(SkipPlatformCheckEnv) == "true" {
		return nil
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if goos != "linux" || goarch != "ppc64le" {
		return fmt.Errorf("podman runtime is only supported on linux/ppc64le platform (current: %s/%s)", goos, goarch)
	}

	return nil
}

// Made with Bob
