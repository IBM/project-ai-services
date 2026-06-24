package runtime

import (
	"io"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

type Runtime interface {
	// Image operations
	ListImages() ([]types.Image, error)
	PullImage(image string) error

	// Pod operations
	ListPods(filters map[string][]string) ([]types.Pod, error)
	CreatePod(body io.Reader, opts map[string]string) ([]types.Pod, error)
	DeletePod(id string, force *bool) error
	StopPod(id string) error
	StartPod(id string) error
	InspectPod(nameOrId string) (*types.Pod, error)
	PodExists(nameOrID string) (bool, error)
	PodLogs(nameOrID string) error
	GetPodResources(nameOrID string) (*types.PodResources, error)

	// Secret operations
	ListSecrets(filters map[string][]string) ([]string, error)
	DeleteSecret(name string) error
	SecretExists(nameOrID string) (bool, error)

	// Volume operations
	DeleteVolume(name string) error
	VolumeExists(nameOrID string) (bool, error)

	// Container operations
	// ListContainers(filters map[string][]string) ([]types.Container, error)
	InspectContainer(nameOrId string) (*types.Container, error)
	ContainerExists(nameOrID string) (bool, error)
	ContainerLogs(containerNameOrID string) error

	// Network operations
	ListRoutes() ([]types.Route, error)

	// PVC operations
	DeletePVCs(appLabel string) error

	// System information
	GetSystemInfo() (*models.SystemInfo, error)

	// Runtime type identification
	Type() types.RuntimeType
}
