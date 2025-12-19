package validators

import (
	"fmt"
	"os/exec"

	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// Podman checks if podman is installed and available in PATH
func Podman() (string, error) {
	path, err := exec.LookPath("podman")
	if err != nil {
		return "", fmt.Errorf("podman is not installed or not found in PATH, error: %v", err)
	}

	return path, nil
}

// PodmanHealthCheck verifies podman is working
func PodmanHealthCheck() error {
	client, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	version, err := system.Version(client.Context, nil)
	if err != nil {
		return fmt.Errorf("podman health check failed (cannot get version): %w", err)
	}

	if version.Server == nil || version.Server.Version == "" {
		return fmt.Errorf("podman health check failed (invalid version info)")
	}

	return nil
}
