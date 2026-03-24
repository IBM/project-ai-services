package utils

import (
	"fmt"
	"runtime"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// CheckPodmanPlatformSupport checks if the current platform supports podman runtime.
// Podman runtime is only supported on linux/ppc64le.
func CheckPodmanPlatformSupport(runtimeType types.RuntimeType) error {
	// Only check if podman runtime is being used
	if runtimeType != types.RuntimeTypePodman {
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
