package podman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/kube"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

type PodmanClient struct {
	Context context.Context
}

// NewPodmanClient creates and returns a new PodmanClient instance
func NewPodmanClient() (*PodmanClient, error) {
	// Default Podman socket URI is unix:///run/podman/podman.sock running on the local machine,
	// but it can be overridden by the CONTAINER_HOST and CONTAINER_SSHKEY environment variable to support remote connections.
	// Please use `podman system connection list` to see available connections.
	// Reference:
	// MacOS instructions runing in a remote VM:
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

// Example function to list images (you can expand with more Podman functionalities)
func (pc *PodmanClient) ListImages() ([]string, error) {
	imagesList, err := images.List(pc.Context, nil)
	if err != nil {
		return nil, err
	}

	var imageNames []string
	for _, img := range imagesList {
		imageNames = append(imageNames, img.ID)
	}
	return imageNames, nil
}

func (pc *PodmanClient) PullImage(image string, options *images.PullOptions) error {
	fmt.Printf("Pulling image %s...\n", image)
	_, err := images.Pull(pc.Context, image, options)
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}
	fmt.Printf("Successfully pulled image %s\n", image)
	return nil
}

func (pc *PodmanClient) ListPods(filters map[string][]string) (any, error) {
	var listOpts pods.ListOptions

	if len(filters) >= 1 {
		listOpts.Filters = filters
	}

	podList, err := pods.List(pc.Context, &listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return podList, nil
}

func (pc *PodmanClient) CreatePod(body io.Reader) (*types.KubePlayReport, error) {
	kubeReport, err := kube.PlayWithBody(pc.Context, body, nil)
	if err != nil {
		return kubeReport, fmt.Errorf("failed to execute podman kube play: %w", err)
	}

	return kubeReport, nil
}

func (pc *PodmanClient) DeletePod(id string, force *bool) error {
	_, err := pods.Remove(pc.Context, id, &pods.RemoveOptions{Force: force})
	if err != nil {
		return fmt.Errorf("failed to delete the pod: %w", err)
	}

	return nil
}

func (pc *PodmanClient) InspectContainer(nameOrId string) (*define.InspectContainerData, error) {
	stats, err := containers.Inspect(pc.Context, nameOrId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if stats == nil {
		return nil, errors.New("got nil stats when doing container inspect")
	}

	return stats, nil
}

func (pc *PodmanClient) ListContainers(filters map[string][]string) (any, error) {
	var listOpts containers.ListOptions

	if len(filters) >= 1 {
		listOpts.Filters = filters
	}

	containerlist, err := containers.List(pc.Context, &listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return containerlist, nil
}

func (pc *PodmanClient) StopPod(id string) error {
	_, err := pods.Stop(pc.Context, id, &pods.StopOptions{})
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

func (pc *PodmanClient) InspectPod(nameOrID string) (*types.PodInspectReport, error) {
	podInspectReport, err := pods.Inspect(pc.Context, nameOrID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the pod: %w", err)
	}
	return podInspectReport, nil
}

func (pc *PodmanClient) PodLogs(podNameOrID string) error {
	if podNameOrID == "" {
		return errors.New("pod name or ID cannot be empty")
	}

	ctx, cancel := context.WithCancel(pc.Context)
	defer cancel()

	// TODO: fetch pods logs via sdk way
	cmdExec := exec.CommandContext(pc.Context, "podman", "pod", "logs", "-f", podNameOrID)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr

	err := cmdExec.Run()

	// If context was cancelled (Ctrl+C), don't treat it as an error
	if ctx.Err() == context.Canceled {
		return nil
	}

	return err
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
				fmt.Println(line)
			case line, ok := <-stderrChan:
				if !ok {
					return
				}
				fmt.Fprintln(os.Stderr, line)
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
