package bootstrap

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// requirePodman resolves the podman binary path or returns an error if not in PATH.
func requirePodman() (string, error) {
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return "", fmt.Errorf("podman not found in PATH: %w", err)
	}

	logger.Infof("[BOOTSTRAP] Podman found at: %s", podmanPath)

	return podmanPath, nil
}

// CheckPodman validates Podman installation & rootless support.
func CheckPodman() error {
	if _, err := requirePodman(); err != nil {
		return err
	}

	output, err := exec.Command("podman", "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get podman version: %w", err)
	}
	logger.Infof("[BOOTSTRAP] Podman version output: %s", string(output))

	if out, err := exec.Command("podman", "info", "--format", "{{.Host.Security.RootlessMode}}").CombinedOutput(); err == nil {
		logger.Infof("[BOOTSTRAP] Rootless mode: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

// PodmanRegistryLogin performs login to the required registry.
func PodmanRegistryLogin(url string, username string, password string) error {
	if _, err := requirePodman(); err != nil {
		return err
	}

	output, err := exec.Command("podman", "login", url, "--username", username, "--password", password).CombinedOutput()
	if err != nil {
		logger.Errorf("[BOOTSTRAP] Registry login failed. Error: %v", err)

		return err
	}

	logger.Infof("[BOOTSTRAP] Registry login successful. Output: %s", string(output))

	return nil
}
