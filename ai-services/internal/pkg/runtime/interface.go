package runtime

import (
	"io"

	"github.com/containers/podman/v5/libpod/define"
	podmanTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

type Runtime interface {
	ListImages() ([]types.Image, error)
	PullImage(image string) error
	ListPods(filters map[string][]string) ([]types.Pod, error)
	CreatePod(body io.Reader) ([]types.Pod, error)
	DeletePod(id string, force *bool) error
	StopPod(id string) error
	StartPod(id string) error
	InspectContainer(nameOrId string) (*define.InspectContainerData, error)
	ListContainers(filters map[string][]string) ([]types.Container, error)
	InspectPod(nameOrId string) (*podmanTypes.PodInspectReport, error)
	PodExists(nameOrID string) (bool, error)
	PodLogs(nameOrID string) error
	ContainerLogs(containerNameOrID string) error
	ContainerExists(nameOrID string) (bool, error)
}
