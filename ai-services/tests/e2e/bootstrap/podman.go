package bootstrap

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// CheckPodman validates Podman installation & rootless support.
func CheckPodman() error {
	// Check if podman is available.
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return fmt.Errorf("podman not found in PATH: %w", err)
	}
	logger.Infof("[BOOTSTRAP] Podman found at: %s", podmanPath)

	// Check Podman version.
	cmd := exec.Command("podman", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get podman version: %w", err)
	}
	logger.Infof("[BOOTSTRAP] Podman version output: %s", string(output))

	// Check rootless support (optional - doesn't fail if not rootless).
	cmd = exec.Command("podman", "info", "--format", "{{.Host.Security.RootlessMode}}")
	output, err = cmd.CombinedOutput()
	if err == nil {
		rootless := strings.TrimSpace(string(output))
		logger.Infof("[BOOTSTRAP] Rootless mode: %s", rootless)
	}

	return nil
}

// PodmanRegistryLogin performs login to the required registry.
func PodmanRegistryLogin(url string, username string, password string) error {
	// Check if podman is available.
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return fmt.Errorf("podman not found in PATH: %w", err)
	}
	logger.Infof("[BOOTSTRAP] Podman found at: %s", podmanPath)

	cmd := exec.Command("podman", "login", url, "--username", username, "--password", password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[BOOTSTRAP] Registry login failed. Error: %v", err)

		return err
	}

	logger.Infof("[BOOTSTRAP] Registry login successful. Output: %s", string(output))

	return nil
}
