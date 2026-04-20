package podman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/kube"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	logChannelBufferSize = 50
)

type PodmanClient struct {
	Context context.Context
}

// NewPodmanClient creates and returns a new PodmanClient instance.
func NewPodmanClient() (*PodmanClient, error) {
	// Default Podman socket URI is unix:///run/podman/podman.sock running on the local machine,
	// but it can be overridden by the CONTAINER_HOST and CONTAINER_SSHKEY environment variable to support remote connections.
	// Please use `podman system connection list` to see available connections.
	// Reference:
	// MacOS instructions running in a remote VM:
	// export CONTAINER_HOST=ssh://root@127.0.0.1:62904/run/podman/podman.sock
	// export CONTAINER_SSHKEY=/Users/manjunath/.local/share/containers/podman/machine/machine
	uri := "unix:///run/podman/podman.sock"
	if v, found := os.LookupEnv("CONTAINER_HOST"); found {
		uri = v
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
	_, err := images.Pull(pc.Context, image, nil)
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

	// Channel to signal goroutine completion
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
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
	}()

	err := containers.Logs(ctx, containerNameOrID, opts, stdoutChan, stderrChan)
	<-done

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}

	return err
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

// Type returns the runtime type for PodmanClient.
func (pc *PodmanClient) Type() types.RuntimeType {
	return types.RuntimeTypePodman
}
