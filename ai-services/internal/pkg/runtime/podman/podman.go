package podman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/kube"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
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

func (pc *PodmanClient) CreatePod(body io.Reader) ([]types.Pod, error) {
	kubeReport, err := kube.PlayWithBody(pc.Context, body, nil)
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

// func (pc *PodmanClient) ListContainers(filters map[string][]string) ([]types.Container, error) {
// 	var listOpts containers.ListOptions

// 	if len(filters) >= 1 {
// 		listOpts.Filters = filters
// 	}

// 	containerlist, err := containers.List(pc.Context, &listOpts)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to list containers: %w", err)
// 	}

// 	return toContainerList(containerlist), nil
// }

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
	//nolint:godox
	// TODO: perform pod start SDK way
	cmdExec := exec.Command("podman", "pod", "start", id)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr

	err := cmdExec.Run()
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

	// Creating context here that listens for Ctrl+C
	ctx, stop := signal.NotifyContext(pc.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := &containers.LogOptions{
		Follow: utils.BoolPtr(true),
		Stderr: utils.BoolPtr(true),
		Stdout: utils.BoolPtr(true),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errChan := make(chan error, len(podInspect.Containers))

	// Start log streaming for each container
	for _, container := range podInspect.Containers {
		wg.Add(1)
		go func(containerID string) {
			defer wg.Done()

			// container prefix to show in logs
			prefix := containerID[:12]

			containerStdout := make(chan string, 50)
			containerStderr := make(chan string, 50)

			// Start a goroutine to handle log output with prefix
			var outputWg sync.WaitGroup
			outputWg.Go(func() {
				for {
					select {
					case <-ctx.Done():
						return
					case line, ok := <-containerStdout:
						if !ok {
							// Check if stderr is also closed
							select {
							case _, ok := <-containerStderr:
								if !ok {
									return
								}
							default:
							}
							continue
						}
						mu.Lock()
						logger.Infof("[%s] %s", prefix, line)
						mu.Unlock()
					case line, ok := <-containerStderr:
						if !ok {
							// Check if stdout is also closed
							select {
							case _, ok := <-containerStdout:
								if !ok {
									return
								}
							default:
							}
							continue
						}
						mu.Lock()
						logger.Errorf("[%s] %s", prefix, line)
						mu.Unlock()
					}
				}
			})

			// Stream logs from container
			err := containers.Logs(ctx, containerID, opts, containerStdout, containerStderr)

			// Close channels to signal output goroutine
			close(containerStdout)
			close(containerStderr)

			// Wait for output goroutine to finish
			outputWg.Wait()

			// Only report errors if context wasn't cancelled
			if err != nil && ctx.Err() == nil {
				errChan <- fmt.Errorf("error streaming logs for container %s: %w", prefix, err)
			}
		}(container.ID)
	}

	// Wait for all container log streams to complete
	wg.Wait()
	close(errChan)

	// Collect and report any errors (only if context wasn't cancelled by user)
	if ctx.Err() != context.Canceled && ctx.Err() != context.DeadlineExceeded {
		for err := range errChan {
			if err != nil {
				logger.Errorln(err.Error())
			}
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

	stdoutChan := make(chan string)
	stderrChan := make(chan string)

	opts := &containers.LogOptions{
		Follow: utils.BoolPtr(true),
		Stderr: utils.BoolPtr(true),
		Stdout: utils.BoolPtr(true),
	}

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
				logger.Errorln(line)
			}
		}
	}()

	err := containers.Logs(ctx, containerNameOrID, opts, stdoutChan, stderrChan)
	<-done
	if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
		return nil
	}

	return err
}

func (pc *PodmanClient) ContainerExists(nameOrID string) (bool, error) {
	return containers.Exists(pc.Context, nameOrID, nil)
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
