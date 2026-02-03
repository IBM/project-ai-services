package runtime

import (
	"io"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

type Runtime interface {
	ListImages() ([]Image, error)
	PullImage(image string) error
	ListPods(filters map[string][]string) ([]Pod, error)
	CreatePod(body io.Reader) ([]Pod, error)
	DeletePod(id string, force *bool) error
	StopPod(id string) error
	StartPod(id string) error
	InspectContainer(nameOrId string) (*define.InspectContainerData, error)
	ListContainers(filters map[string][]string) ([]Container, error)
	InspectPod(nameOrId string) (*types.PodInspectReport, error)
	PodExists(nameOrID string) (bool, error)
	PodLogs(nameOrID string) error
	ContainerLogs(containerNameOrID string) error
	ContainerExists(nameOrID string) (bool, error)
}
