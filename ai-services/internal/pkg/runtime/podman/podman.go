package podman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/kube"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/bindings/secrets"
	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/project-ai-services/ai-services/internal/pkg/accelerator/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	logChannelBufferSize      = 50
	execCommandFixedArgsCount = 2 // "exec" and containerID
)

type PodmanClient struct {
	Context context.Context
}

// NewPodmanClient creates and returns a new PodmanClient instance.
func NewPodmanClient() (*PodmanClient, error) {
	// Set XDG_RUNTIME_DIR for non-root users if not already set
	// This is required for rootless Podman to access runtime directories
	euid := os.Geteuid()
	if euid != 0 && os.Getenv("XDG_RUNTIME_DIR") == "" {
		uid := os.Getuid()
		logger.Debugf("Running as non-root user %d, setting XDG_RUNTIME_DIR", uid)
		if err := os.Setenv("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", uid)); err != nil {
			return nil, fmt.Errorf("failed to set XDG_RUNTIME_DIR: %w", err)
		}
	}

	// Default Podman socket URI is unix:///run/podman/podman.sock running on the local machine,
	// but it can be overridden by the CONTAINER_HOST and CONTAINER_SSHKEY environment variable to support remote connections.
	// Please use `podman system connection list` to see available connections.
	// Reference:
	// MacOS instructions running in a remote VM:
	// export CONTAINER_HOST=ssh://root@127.0.0.1:62904/run/podman/podman.sock
	// export CONTAINER_SSHKEY=/Users/manjunath/.local/share/containers/podman/machine/machine
	uri, err := utils.ResolvePodmanURI()
	if err != nil {
		return nil, err
	}

	ctx, err := bindings.NewConnection(context.Background(), uri)
	if err != nil {
		return nil, err
	}

	return &PodmanClient{Context: ctx}, nil
}

// ListImages function to list images (you can expand with more Podman functionalities).
func (pc *PodmanClient) ListImages() ([]types.Image, error) {
	images, err := images.List(pc.Context, nil)
	if err != nil {
		return nil, err
	}

	return toImageList(images), nil
}

func (pc *PodmanClient) PullImage(image string) error {
	logger.Infof("Pulling image %s...\n", image)

	// Create pull options with auth file from environment
	opts := &images.PullOptions{}
	if authFile := os.Getenv("REGISTRY_AUTH_FILE"); authFile != "" {
		opts.Authfile = &authFile
	}

	_, err := images.Pull(pc.Context, image, opts)
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}
	logger.Infof("Successfully pulled image %s\n", image)

	return nil
}

func (pc *PodmanClient) ListPods(filters map[string][]string) ([]types.Pod, error) {
	var listOpts pods.ListOptions

	if len(filters) >= 1 {
		listOpts.Filters = filters
	}

	podList, err := pods.List(pc.Context, &listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return toPodsList(podList), nil
}

func (pc *PodmanClient) CreatePod(body io.Reader, opts map[string]string) ([]types.Pod, error) {
	options := &kube.PlayOptions{}

	// Handle start option
	if v, ok := opts["start"]; ok {
		switch v {
		case constants.PodStartOff:
			start := false
			options.Start = &start
		case constants.PodStartOn:
			start := true
			options.Start = &start
		default:
			// by default go with start set to true
			start := true
			options.Start = &start
		}
	}

	// Handle publish option
	if v, ok := opts["publish"]; ok {
		portMappings := strings.Split(v, ",")
		publishPorts := []string{}
		for _, portMapping := range portMappings {
			if portMapping != "" {
				publishPorts = append(publishPorts, portMapping)
			}
		}
		if len(publishPorts) > 0 {
			options.PublishPorts = publishPorts
		}
	}

	kubeReport, err := kube.PlayWithBody(pc.Context, body, options)
	if err != nil {
		return nil, fmt.Errorf("failed to execute podman kube play: %w", err)
	}

	return toPodsList(kubeReport), nil
}

func (pc *PodmanClient) DeletePod(id string, force *bool) error {
	_, err := pods.Remove(pc.Context, id, &pods.RemoveOptions{Force: force})
	if err != nil {
		return fmt.Errorf("failed to delete the pod: %w", err)
	}

	return nil
}

func (pc *PodmanClient) InspectContainer(nameOrId string) (*types.Container, error) {
	stats, err := containers.Inspect(pc.Context, nameOrId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if stats == nil {
		return nil, errors.New("got nil stats when doing container inspect")
	}

	return toInspectContainer(stats), nil
}

func (pc *PodmanClient) StopPod(id string) error {
	inspectReport, err := pc.InspectPod(id)
	if err != nil {
		return fmt.Errorf("failed to inspect pod: %w", err)
	}

	for _, container := range inspectReport.Containers {
		// skipping infra container as it will be stopped when other containers are stopped
		if container.ID != inspectReport.InfraContainerID {
			err := containers.Stop(pc.Context, container.ID, nil)
			if err != nil {
				return fmt.Errorf("failed to stop pod container %s; err: %w", container.ID, err)
			}
		}
	}
	_, err = pods.Stop(pc.Context, id, &pods.StopOptions{})
	if err != nil {
		return fmt.Errorf("failed to stop the pod: %w", err)
	}

	return nil
}

func (pc *PodmanClient) StartPod(id string) error {
	_, err := pods.Start(pc.Context, id, &pods.StartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start the pod: %w", err)
	}

	return nil
}

func (pc *PodmanClient) InspectPod(nameOrID string) (*types.Pod, error) {
	podInspectReport, err := pods.Inspect(pc.Context, nameOrID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the pod: %w", err)
	}

	return toPodInspectReport(podInspectReport), nil
}

// streamContainerLogs streams logs from a container using channels.
func (pc *PodmanClient) streamContainerLogs(ctx context.Context, containerNameOrID string) error {
	opts := &containers.LogOptions{
		Follow: utils.BoolPtr(true),
		Stderr: utils.BoolPtr(true),
		Stdout: utils.BoolPtr(true),
	}

	stdoutChan := make(chan string, logChannelBufferSize)
	stderrChan := make(chan string, logChannelBufferSize)

	logsCtx, cancelLogs := context.WithCancel(ctx)
	defer cancelLogs()

	// Channel to signal goroutine completion
	done := make(chan struct{})

	go func() {
		defer close(done)
		waitDone := make(chan struct{})
		go func() {
			defer close(waitDone)
			_, err := containers.Wait(ctx, containerNameOrID, nil)
			if err == nil {
				// Container exited, cancel the logs streaming
				cancelLogs()
			}
		}()

		// Stream logs
		_ = containers.Logs(logsCtx, containerNameOrID, opts, stdoutChan, stderrChan)

		// Wait for container wait to complete
		<-waitDone
	}()

	// passing both contexts so it respects Ctrl+C and container exit
	pc.printLogsFromChannels(ctx, logsCtx, stdoutChan, stderrChan)

	// Wait for goroutine to complete
	<-done

	return nil
}

// printLogsFromChannels reads from stdout and stderr channels and prints logs.
func (pc *PodmanClient) printLogsFromChannels(parentCtx, logsCtx context.Context, stdoutChan, stderrChan <-chan string) {
	for {
		select {
		case <-parentCtx.Done():
			// Parent context cancelled (e.g., Ctrl+C)
			return
		case <-logsCtx.Done():
			// Logs context cancelled (e.g., container exited)
			return
		case line, ok := <-stdoutChan:
			if !ok {
				return
			}
			logger.Infoln(line)
		case line, ok := <-stderrChan:
			if !ok {
				return
			}
			logger.Infoln(line)
		}
	}
}

func (pc *PodmanClient) PodLogs(podNameOrID string) error {
	if podNameOrID == "" {
		return errors.New("pod name or ID cannot be empty")
	}

	podInspect, err := pc.InspectPod(podNameOrID)
	if err != nil {
		return fmt.Errorf("failed to inspect pod: %w", err)
	}

	if len(podInspect.Containers) == 0 {
		return errors.New("no containers found in pod")
	}

	// creating context here that listens for Ctrl+C
	ctx, stop := signal.NotifyContext(pc.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	for _, container := range podInspect.Containers {
		// Skip infra container
		if container.ID == podInspect.InfraContainerID {
			continue
		}

		logger.Infof("Streaming logs for container: %s", container.Name)

		if err := pc.streamContainerLogs(ctx, container.ID); err != nil {
			return fmt.Errorf("error reading logs for container %s: %w", container.Name, err)
		}

		// Check if context was cancelled
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			return nil
		}
	}

	return nil
}

func (pc *PodmanClient) PodExists(nameOrID string) (bool, error) {
	return pods.Exists(pc.Context, nameOrID, nil)
}

func (pc *PodmanClient) ContainerLogs(containerNameOrID string) error {
	if containerNameOrID == "" {
		return fmt.Errorf("container name or ID required to fetch logs")
	}

	// Creating context here that listens for Ctrl+C
	ctx, stop := signal.NotifyContext(pc.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return pc.streamContainerLogs(ctx, containerNameOrID)
}

func (pc *PodmanClient) ContainerExists(nameOrID string) (bool, error) {
	return containers.Exists(pc.Context, nameOrID, nil)
}

// RunContainerWithSpec creates, starts, waits for, and removes a container with the given spec.
// Returns the exit code of the container.
func (pc *PodmanClient) RunContainerWithSpec(s *specgen.SpecGenerator) (int32, error) {
	// Create container
	createResponse, err := containers.CreateWithSpec(pc.Context, s, nil)
	if err != nil {
		return -1, fmt.Errorf("failed to create container: %w", err)
	}

	containerID := createResponse.ID

	// Start container
	if err := containers.Start(pc.Context, containerID, nil); err != nil {
		return -1, fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container to complete
	exitCode, err := containers.Wait(pc.Context, containerID, nil)
	if err != nil {
		return -1, fmt.Errorf("failed to wait for container: %w", err)
	}

	return exitCode, nil
}

func (pc *PodmanClient) ListRoutes() ([]types.Route, error) {
	logger.Errorf("unsupported method called!")

	return nil, fmt.Errorf("unsupported method")
}

func (pc *PodmanClient) DeletePVCs(appLabel string) error {
	logger.Errorf("unsupported method called!")

	return fmt.Errorf("unsupported method")
}

func (pc *PodmanClient) DeleteSecret(name string) error {
	err := secrets.Remove(pc.Context, name)
	if err != nil {
		return fmt.Errorf("failed to remove secret: %w", err)
	}

	return nil
}

func (pc *PodmanClient) DeleteVolume(name string) error {
	err := volumes.Remove(pc.Context, name, nil)
	if err != nil {
		return fmt.Errorf("failed to remove volume: %w", err)
	}

	return nil
}

func (pc *PodmanClient) VolumeExists(nameOrID string) (bool, error) {
	return volumes.Exists(pc.Context, nameOrID, nil)
}

func (pc *PodmanClient) ListSecrets(filters map[string][]string) ([]string, error) {
	var listOpts secrets.ListOptions
	if len(filters) >= 1 {
		listOpts.Filters = filters
	}

	secretList, err := secrets.List(pc.Context, &listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	secretIDorNames := make([]string, 0, len(secretList))
	for _, sec := range secretList {
		secretIDorNames = append(secretIDorNames, sec.ID)
	}

	return secretIDorNames, nil
}

func (pc *PodmanClient) SecretExists(nameOrID string) (bool, error) {
	return secrets.Exists(pc.Context, nameOrID)
}

// Type returns the runtime type for PodmanClient.
func (pc *PodmanClient) Type() types.RuntimeType {
	return types.RuntimeTypePodman
}

// GetSystemInfo retrieves system resource information including CPU, memory, and accelerators.
func (pc *PodmanClient) GetSystemInfo() (*models.SystemInfo, error) {
	sysInfo := &models.SystemInfo{}

	// Get Podman system info for CPU and memory
	info, err := system.Info(pc.Context, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Extract CPU and memory information
	if info.Host != nil {
		totalCores := int(info.Host.CPUs)
		idlePercent := 0.0

		if info.Host.CPUUtilization != nil {
			idlePercent = info.Host.CPUUtilization.IdlePercent
		}

		// Calculate available cores: available_cores = (total_cores * idle_percent) / 100
		availableCores := (float64(totalCores) * idlePercent) / constants.PercentageDivisor

		sysInfo.CPU = &models.CPUInfo{
			TotalCores:     totalCores,
			AvailableCores: availableCores,
		}

		sysInfo.Memory = &models.MemoryInfo{
			TotalBytes:     info.Host.MemTotal,
			AvailableBytes: info.Host.MemFree,
		}
	}

	// Populate accelerator information (Spyre cards)
	sysInfo.Accelerators = getAcceleratorInfo(pc.Context)

	return sysInfo, nil
}

// getAcceleratorInfo retrieves accelerator availability information for Podman.
func getAcceleratorInfo(ctx context.Context) map[string]*models.AcceleratorInfo {
	accelerators := make(map[string]*models.AcceleratorInfo)

	// Get total Spyre cards
	totalCards, err := spyre.ListCards(ctx)
	if err != nil {
		logger.ErrorfCtx(ctx, "Could not list Spyre cards: %v", err)
		// Return empty map when error occurs
		return accelerators
	}

	totalCount := len(totalCards)
	if totalCount == 0 {
		// Return empty map when no Spyre cards found
		return accelerators
	}

	// Get available Spyre cards
	availableCards, err := spyre.FindFreeCards(ctx)
	if err != nil {
		logger.ErrorfCtx(ctx, "Could not find available Spyre cards: %v", err)
		accelerators["ibm.com/spyre_pf"] = &models.AcceleratorInfo{
			Total:     totalCount,
			Available: 0,
		}

		return accelerators
	}

	availableCount := len(availableCards)

	accelerators["ibm.com/spyre_pf"] = &models.AcceleratorInfo{
		Total:     totalCount,
		Available: availableCount,
	}

	return accelerators
}

// GetPodResources retrieves resource usage and Spyre cards for a pod in a single call.
func (pc *PodmanClient) GetPodResources(nameOrID string) (*types.PodResources, error) {
	// Inspect the pod to get its details
	podInspect, err := pods.Inspect(pc.Context, nameOrID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod: %w", err)
	}

	if len(podInspect.Containers) == 0 {
		return &types.PodResources{
			CPUCores:   0,
			MemUsage:   0,
			SpyreCards: []string{},
		}, nil
	}

	// Get stats and Spyre cards for all containers in the pod (excluding infra container)
	return pc.aggregateContainerResourcesWithStats(podInspect)
}

// aggregateContainerResourcesWithStats collects and aggregates resources from all non-infra containers using podman stats.
func (pc *PodmanClient) aggregateContainerResourcesWithStats(podInspect *entities.PodInspectReport) (*types.PodResources, error) {
	var totalMemUsage uint64
	var totalCPUCores float64
	spyreCards := []string{}

	for _, container := range podInspect.Containers {
		// Skip infra container
		if container.ID == podInspect.InfraContainerID {
			continue
		}

		// Get container stats for actual CPU and memory usage using podman stats
		statsChan, err := containers.Stats(pc.Context, []string{container.ID}, &containers.StatsOptions{
			Stream: utils.BoolPtr(false), // Get a single snapshot, not streaming
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get stats for container %s: %w", container.Name, err)
		}

		// Read from the stats channel (non-streaming mode returns one report)
		statsReport, ok := <-statsChan
		if ok && statsReport.Error != nil {
			return nil, fmt.Errorf("error in stats report for container %s: %v", container.Name, statsReport.Error)
		}
		if ok && len(statsReport.Stats) > 0 {
			stats := statsReport.Stats[0]

			// Accumulate memory usage (in bytes)
			totalMemUsage += stats.MemUsage

			// Accumulate CPU usage
			// The CPU field is a percentage (e.g., 150.0 = 1.5 cores)
			// Convert percentage to cores by dividing by 100
			totalCPUCores += stats.CPU / constants.PercentageDivisor
		}

		// Inspect container to get Spyre card annotations
		containerInspect, err := containers.Inspect(pc.Context, container.ID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
		}

		// Collect Spyre card PCI addresses from annotations
		collectSpyreCards(containerInspect, &spyreCards)
	}

	return &types.PodResources{
		CPUCores:   totalCPUCores,
		MemUsage:   totalMemUsage,
		SpyreCards: spyreCards,
	}, nil
}

// collectSpyreCards extracts Spyre card PCI addresses from container environment variables.
func collectSpyreCards(containerInspect *define.InspectContainerData, spyreCards *[]string) {
	if containerInspect.Config != nil && containerInspect.Config.Env != nil {
		for _, env := range containerInspect.Config.Env {
			pciAddressPrefix := string(constants.PCIAddressKey) + "="
			if strings.HasPrefix(env, pciAddressPrefix) {
				// Extract the value after "AIU_PCIE_IDS="
				pciAddresses := strings.TrimPrefix(env, pciAddressPrefix)
				// Split by spaces and filter out empty strings
				addresses := strings.Fields(pciAddresses)
				for _, addr := range addresses {
					if addr != "" {
						*spyreCards = append(*spyreCards, addr)
					}
				}

				return
			}
		}
	}
}

// ExecInContainer executes a command in a container using podman exec command.
// Note: Using exec.Command instead of SDK because the SDK's exec API is complex
// and requires handlers.ExecCreateConfig which is not easily accessible.
func (pc *PodmanClient) ExecInContainer(containerID string, cmd []string) error {
	// Build podman exec command
	args := make([]string, 0, execCommandFixedArgsCount+len(cmd))
	args = append(args, "exec", containerID)
	args = append(args, cmd...)

	execCmd := exec.CommandContext(pc.Context, "podman", args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// ExecInContainerWithOutput executes a command in a container and returns the output.
func (pc *PodmanClient) ExecInContainerWithOutput(containerID string, cmd []string) (string, error) {
	// Build podman exec command
	args := make([]string, 0, execCommandFixedArgsCount+len(cmd))
	args = append(args, "exec", containerID)
	args = append(args, cmd...)

	execCmd := exec.CommandContext(pc.Context, "podman", args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	return string(output), nil
}

// ExecInContainerWithEnv executes a command in a container with environment variables.
// This is used to pass sensitive data like passwords without exposing them in process lists.
// Environment variables are set inline in the shell command to avoid exposure.
func (pc *PodmanClient) ExecInContainerWithEnv(containerID string, env map[string]string, script string) error {
	// Build environment variable assignments for the shell
	envVars := make([]string, 0, len(env))
	for key, value := range env {
		// Use single quotes to prevent shell expansion, escape any single quotes in the value
		escapedValue := strings.ReplaceAll(value, "'", "'\\''")
		envVars = append(envVars, fmt.Sprintf("%s='%s'", key, escapedValue))
	}

	// Combine env vars with the script
	fullScript := strings.Join(envVars, " ") + " " + script

	return pc.ExecInContainer(containerID, []string{"sh", "-c", fullScript})
}

// CopyDirToContainer copies a directory to a container using podman cp command.
// Note: Using exec.Command instead of SDK because the SDK's copy API requires
// tar archive handling which is complex.
func (pc *PodmanClient) CopyDirToContainer(containerID, srcDir, destDir string) error {
	// Verify source directory exists
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("source directory does not exist: %s", srcDir)
	}

	// Use podman cp command to copy directory
	// Format: podman cp <src>/. <container>:<dest>
	// The "/." ensures we copy the contents of the directory, not the directory itself
	cpCmd := exec.CommandContext(pc.Context, "podman", "cp", srcDir+"/.", fmt.Sprintf("%s:%s", containerID, destDir))
	output, err := cpCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to copy directory: %w, output: %s", err, string(output))
	}

	return nil
}

// CreateSidecarContainer creates a sidecar container in the specified pod.
// Returns the container ID of the created sidecar.
func (pc *PodmanClient) CreateSidecarContainer(podID, sidecarName, image string, command []string) (string, error) {
	s := &specgen.SpecGenerator{
		ContainerBasicConfig: specgen.ContainerBasicConfig{
			Name:    sidecarName,
			Remove:  utils.BoolPtr(true), // Auto-remove container when stopped
			Command: command,
			Pod:     podID,
		},
		ContainerStorageConfig: specgen.ContainerStorageConfig{
			Image: image,
		},
		ContainerHealthCheckConfig: specgen.ContainerHealthCheckConfig{
			// Set HealthConfig to nil to disable health checks
			HealthConfig: nil,
			// Set HealthLogDestination to /tmp to satisfy directory requirement
			HealthLogDestination: "/tmp",
		},
	}

	createResponse, err := containers.CreateWithSpec(pc.Context, s, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create sidecar container: %w", err)
	}

	containerID := createResponse.ID
	if err := containers.Start(pc.Context, containerID, nil); err != nil {
		return "", fmt.Errorf("failed to start sidecar container: %w", err)
	}

	return containerID, nil
}

// StopContainer stops a container by ID.
func (pc *PodmanClient) StopContainer(containerID string) error {
	return containers.Stop(pc.Context, containerID, nil)
}

// SidecarExecutor is a function type that performs operations using a sidecar container.
type SidecarExecutor func(ctx context.Context, containerID string) error

// ManageSidecarLifecycle manages the complete lifecycle of a sidecar container.
// It creates the sidecar, executes the provided function, and ensures cleanup.
func (pc *PodmanClient) ManageSidecarLifecycle(podID, sidecarName, image string, command []string, executor SidecarExecutor) error {
	// Create and start sidecar container
	containerID, err := pc.CreateSidecarContainer(podID, sidecarName, image, command)
	if err != nil {
		return fmt.Errorf("failed to create and start sidecar: %w", err)
	}

	// Ensure cleanup happens
	defer func() {
		logger.Infoln("Cleaning up sidecar container...")
		stopErr := pc.StopContainer(containerID)
		if stopErr != nil {
			logger.Warningf("Failed to stop sidecar container %s: %v\n", containerID, stopErr)
		}
		// Note: Container has Remove=true, so it will be auto-removed when stopped
		logger.Infoln("Sidecar container cleanup completed")
	}()

	// Execute the provided function with the sidecar
	return executor(pc.Context, containerID)
}
