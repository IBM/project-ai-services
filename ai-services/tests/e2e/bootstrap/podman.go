package bootstrap

import (
	"fmt"
	"os/exec"
	"strings"
)

// CheckPodman validates Podman installation & rootless support.
func CheckPodman() error {
	// Check if podman is available.
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return fmt.Errorf("podman not found in PATH: %w", err)
	}
	fmt.Printf("[BOOTSTRAP] Podman found at: %s\n", podmanPath)

	// Check Podman version.
	cmd := exec.Command("podman", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get podman version: %w", err)
	}
	fmt.Printf("[BOOTSTRAP] %s", string(output))

	// Check rootless support (optional - doesn't fail if not rootless).
	cmd = exec.Command("podman", "info", "--format", "{{.Host.Security.RootlessMode}}")
	output, err = cmd.CombinedOutput()
	if err == nil {
		rootless := strings.TrimSpace(string(output))
		fmt.Printf("[BOOTSTRAP] Rootless mode: %s\n", rootless)
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
	fmt.Printf("Podman found at: %s\n", podmanPath)

	cmd := exec.Command("podman", "login", url, "--username", username, "--password", password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Registry login failed. Error: %v\n", err)

		return err
	}

	fmt.Printf("Registry login successful. Output: %s", string(output))

	return nil
}
