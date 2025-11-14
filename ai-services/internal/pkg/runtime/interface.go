package runtime

import (
	"io"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

type Runtime interface {
	ListImages() ([]string, error)
	PullImage(image string, options *images.PullOptions) error
	ListPods(filters map[string][]string) (any, error)
	CreatePod(body io.Reader) (*types.KubePlayReport, error)
	DeletePod(id string, force *bool) error
	StopPod(id string) error
	StartPod(id string) error
	InspectContainer(nameOrId string) (*define.InspectContainerData, error)
	ListContainers(filters map[string][]string) (any, error)
	InspectPod(nameOrId string) (*types.PodInspectReport, error)
	PodExists(nameOrID string) (bool, error)
	PodLogs(nameOrID string) error
	ContainerLogs(containerNameOrID string) error
	ContainerExists(nameOrID string) (bool, error)
}
