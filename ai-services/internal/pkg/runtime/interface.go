package runtime

import (
	"io"

	"github.com/containers/podman/v5/libpod/define"
	podmanTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Runtime defines the interface for container runtime operations
// This interface abstracts the underlying runtime (Podman, Kubernetes, etc.)
type Runtime interface {
	// Image operations
	ListImages() ([]types.Image, error)
	PullImage(image string) error

	// Pod operations
	ListPods(filters map[string][]string) ([]types.Pod, error)
	CreatePod(body io.Reader) ([]types.Pod, error)
	DeletePod(id string, force *bool) error
	InspectPod(nameOrID string) (*podmanTypes.PodInspectReport, error)
	PodExists(nameOrID string) (bool, error)
	StopPod(id string) error
	StartPod(id string) error
	PodLogs(podNameOrID string) error

	// Container operations
	ListContainers(filters map[string][]string) ([]types.Container, error)
	InspectContainer(nameOrId string) (*define.InspectContainerData, error)
	ContainerExists(nameOrID string) (bool, error)
	ContainerLogs(containerNameOrID string) error

	// Runtime type identification
	Type() types.RuntimeType
}

// Made with Bob
