package mustgather

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// PodmanMustGatherer implements MustGatherer for Podman runtime.
type PodmanMustGatherer struct {
	sanitizer *SecretSanitizer
}

// NewPodmanMustGatherer creates a new PodmanMustGatherer.
func NewPodmanMustGatherer() *PodmanMustGatherer {
	return &PodmanMustGatherer{
		sanitizer: NewSecretSanitizer(),
	}
}

// Gather collects debugging information for Podman runtime.
func (p *PodmanMustGatherer) Gather(opts MustGatherOptions) error {
	logger.Infoln("Starting must-gather for Podman runtime...")

	// Create output directory with numeric ID (nanosecond timestamp)
	numericID := time.Now().UnixNano()
	outputDir := filepath.Join(opts.OutputDir, fmt.Sprintf("must-gather.local.%d", numericID))
	if err := os.MkdirAll(outputDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	logger.Infof("Output directory: %s\n", outputDir)

	// Collect system information
	if err := p.collectSystemInfo(outputDir); err != nil {
		logger.Warningf("Failed to collect system info: %v\n", err)
	}

	// Collect pod information
	if err := p.collectPodInfo(outputDir, opts.ApplicationName); err != nil {
		logger.Warningf("Failed to collect pod info: %v\n", err)
	}

	// Collect container information
	if err := p.collectContainerInfo(outputDir, opts.ApplicationName); err != nil {
		logger.Warningf("Failed to collect container info: %v\n", err)
	}

	// Collect logs
	if err := p.collectLogs(outputDir, opts.ApplicationName); err != nil {
		logger.Warningf("Failed to collect logs: %v\n", err)
	}

	// Collect network information
	if err := p.collectNetworkInfo(outputDir); err != nil {
		logger.Warningf("Failed to collect network info: %v\n", err)
	}

	// Collect volume information
	if err := p.collectVolumeInfo(outputDir); err != nil {
		logger.Warningf("Failed to collect volume info: %v\n", err)
	}

	logger.Infoln("Must-gather collection completed")

	return nil
}

// collectSystemInfo collects system-level information.
func (p *PodmanMustGatherer) collectSystemInfo(outputDir string) error {
	logger.Infoln("Collecting system information...")

	commands := map[string][]string{
		"podman-version.txt": {"podman", "version"},
		"podman-info.json":   {"podman", "info", "--format", "json"},
		"system-df.txt":      {"podman", "system", "df"},
	}

	for filename, cmd := range commands {
		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			logger.Warningf("Failed to run %s: %v\n", strings.Join(cmd, " "), err)

			continue
		}

		// Sanitize output
		sanitized := p.sanitizer.SanitizeText(string(output))
		if err := p.writeFile(outputDir, filename, []byte(sanitized)); err != nil {
			return err
		}
	}

	return nil
}

// collectPodInfo collects pod information.
func (p *PodmanMustGatherer) collectPodInfo(outputDir, appName string) error {
	logger.Infoln("Collecting pod information...")

	// List all pods
	cmd := []string{"podman", "pod", "ps", "--format", "json"}
	output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Parse and filter pods
	var pods []map[string]interface{}
	if err := json.Unmarshal(output, &pods); err != nil {
		return fmt.Errorf("failed to parse pod list: %w", err)
	}

	podDir := filepath.Join(outputDir, "pods")
	if err := os.MkdirAll(podDir, dirPermissions); err != nil {
		return err
	}

	for _, pod := range pods {
		podName, ok := pod["Name"].(string)
		if !ok {
			continue
		}

		// Filter by application name if specified
		if appName != "" && !strings.Contains(podName, appName) {
			continue
		}

		// Get detailed pod inspect
		inspectCmd := []string{"podman", "pod", "inspect", podName}
		inspectOutput, err := exec.Command(inspectCmd[0], inspectCmd[1:]...).CombinedOutput()
		if err != nil {
			logger.Warningf("Failed to inspect pod %s: %v\n", podName, err)

			continue
		}

		// Sanitize and save
		sanitized, _ := p.sanitizer.SanitizeJSON(inspectOutput)
		filename := fmt.Sprintf("%s-inspect.json", podName)
		if err := p.writeFile(podDir, filename, sanitized); err != nil {
			logger.Warningf("Failed to write pod inspect for %s: %v\n", podName, err)
		}
	}

	return nil
}

// collectContainerInfo collects container information.
func (p *PodmanMustGatherer) collectContainerInfo(outputDir, appName string) error {
	logger.Infoln("Collecting container information...")

	containers, err := p.listContainers()
	if err != nil {
		return err
	}

	containerDir := filepath.Join(outputDir, "containers")
	if err := os.MkdirAll(containerDir, dirPermissions); err != nil {
		return err
	}

	for _, container := range containers {
		p.processContainer(container, containerDir, appName)
	}

	return nil
}

// listContainers lists all podman containers.
func (p *PodmanMustGatherer) listContainers() ([]map[string]interface{}, error) {
	cmd := []string{"podman", "ps", "-a", "--format", "json"}
	output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container list: %w", err)
	}

	return containers, nil
}

// processContainer processes a single container and saves its information.
func (p *PodmanMustGatherer) processContainer(container map[string]interface{}, containerDir, appName string) {
	containerID, ok := container["Id"].(string)
	if !ok {
		return
	}

	containerName := p.getContainerName(container)
	if containerName == "" {
		return
	}

	// Filter by application name if specified
	if appName != "" && !strings.Contains(containerName, appName) {
		return
	}

	p.inspectAndSaveContainer(containerID, containerName, containerDir)
}

// getContainerName extracts the container name from container data.
func (p *PodmanMustGatherer) getContainerName(container map[string]interface{}) string {
	names, ok := container["Names"].([]interface{})
	if !ok || len(names) == 0 {
		return ""
	}

	return fmt.Sprintf("%v", names[0])
}

// inspectAndSaveContainer inspects a container and saves its details.
func (p *PodmanMustGatherer) inspectAndSaveContainer(containerID, containerName, containerDir string) {
	inspectCmd := []string{"podman", "inspect", containerID}
	inspectOutput, err := exec.Command(inspectCmd[0], inspectCmd[1:]...).CombinedOutput()
	if err != nil {
		logger.Warningf("Failed to inspect container %s: %v\n", containerName, err)

		return
	}

	// Sanitize and save
	sanitized, _ := p.sanitizer.SanitizeJSON(inspectOutput)
	filename := fmt.Sprintf("%s-inspect.json", containerName)
	if err := p.writeFile(containerDir, filename, sanitized); err != nil {
		logger.Warningf("Failed to write container inspect for %s: %v\n", containerName, err)
	}
}

// collectLogs collects container logs.
func (p *PodmanMustGatherer) collectLogs(outputDir, appName string) error {
	logger.Infoln("Collecting container logs...")

	// List all containers
	cmd := []string{"podman", "ps", "-a", "--format", "{{.Names}}"}
	output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	logsDir := filepath.Join(outputDir, "logs")
	if err := os.MkdirAll(logsDir, dirPermissions); err != nil {
		return err
	}

	containerNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, containerName := range containerNames {
		if containerName == "" {
			continue
		}

		// Filter by application name if specified
		if appName != "" && !strings.Contains(containerName, appName) {
			continue
		}

		// Get container logs (last 1000 lines)
		logsCmd := []string{"podman", "logs", "--tail", "1000", containerName}
		logsOutput, err := exec.Command(logsCmd[0], logsCmd[1:]...).CombinedOutput()
		if err != nil {
			logger.Warningf("Failed to get logs for container %s: %v\n", containerName, err)

			continue
		}

		// Sanitize and save
		sanitized := p.sanitizer.SanitizeText(string(logsOutput))
		filename := fmt.Sprintf("%s.log", containerName)
		if err := p.writeFile(logsDir, filename, []byte(sanitized)); err != nil {
			logger.Warningf("Failed to write logs for %s: %v\n", containerName, err)
		}
	}

	return nil
}

// collectNetworkInfo collects network information.
func (p *PodmanMustGatherer) collectNetworkInfo(outputDir string) error {
	logger.Infoln("Collecting network information...")

	networkDir := filepath.Join(outputDir, "network")
	if err := os.MkdirAll(networkDir, dirPermissions); err != nil {
		return err
	}

	// List networks
	cmd := []string{"podman", "network", "ls", "--format", "json"}
	output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	sanitized, _ := p.sanitizer.SanitizeJSON(output)
	if err := p.writeFile(networkDir, "networks.json", sanitized); err != nil {
		return err
	}

	return nil
}

// collectVolumeInfo collects volume information.
func (p *PodmanMustGatherer) collectVolumeInfo(outputDir string) error {
	logger.Infoln("Collecting volume information...")

	volumeDir := filepath.Join(outputDir, "volumes")
	if err := os.MkdirAll(volumeDir, dirPermissions); err != nil {
		return err
	}

	// List volumes
	cmd := []string{"podman", "volume", "ls", "--format", "json"}
	output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list volumes: %w", err)
	}

	sanitized, _ := p.sanitizer.SanitizeJSON(output)
	if err := p.writeFile(volumeDir, "volumes.json", sanitized); err != nil {
		return err
	}

	return nil
}

// writeFile writes content to a file in the specified directory.
func (p *PodmanMustGatherer) writeFile(dir, filename string, content []byte) error {
	filepath := filepath.Join(dir, filename)

	return os.WriteFile(filepath, content, filePermissions)
}

// Made with Bob
