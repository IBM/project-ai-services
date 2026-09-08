package runtime

import (
	"context"
	"io"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Runtime interface {
	// Image operations
	ListImages(ctx context.Context) ([]types.Image, error)
	PullImage(ctx context.Context, image string) error

	// Pod operations
	ListPods(ctx context.Context, filters map[string][]string) ([]types.Pod, error)
	CreatePod(ctx context.Context, body io.Reader, opts map[string]string) ([]types.Pod, error)
	DeletePod(ctx context.Context, id string, force *bool) error
	StopPod(ctx context.Context, id string) error
	StartPod(ctx context.Context, id string) error
	InspectPod(ctx context.Context, nameOrId string) (*types.Pod, error)
	PodExists(ctx context.Context, nameOrID string) (bool, error)
	PodLogs(ctx context.Context, nameOrID string) error
	GetPodResources(ctx context.Context, nameOrID string) (*types.PodResources, error)
	GetNamespace(ctx context.Context) (string, error)

	// Secret operations
	ListSecrets(ctx context.Context, filters map[string][]string) ([]string, error)
	DeleteSecret(ctx context.Context, name string) error
	SecretExists(ctx context.Context, nameOrID string) (bool, error)
	UpdateSecret(ctx context.Context, name, deploymentName string, data map[string][]byte) error

	// Volume operations
	DeleteVolume(ctx context.Context, name string) error
	VolumeExists(ctx context.Context, nameOrID string) (bool, error)

	// Container operations
	// ListContainers(ctx context.Context, filters map[string][]string) ([]types.Container, error)
	InspectContainer(ctx context.Context, nameOrId string) (*types.Container, error)
	ContainerExists(ctx context.Context, nameOrID string) (bool, error)
	ContainerLogs(ctx context.Context, containerNameOrID string) error
	ExecInContainerWithCmd(ctx context.Context, podName, containerName string, command []string) (string, error)

	// Network operations
	ListRoutes(ctx context.Context, labelSelector string) ([]types.Route, error)

	// ListCRD populates crd list
	// resources in the namespace that carry every label key in filters["label"].
	ListCRD(ctx context.Context, list *unstructured.UnstructuredList, filters map[string][]string) ([]types.CRDResource, error)

	// Namespace operations
	DeleteNamespace(ctx context.Context, name string) error

	// PVC operations
	DeletePVCs(ctx context.Context, appLabel string) error

	// System information
	GetSystemInfo(ctx context.Context) (*models.SystemInfo, error)

	// HTTPProxy tunnels an HTTP request through the gRPC stream to a worker
	// pod endpoint and returns the response.
	// method is the HTTP verb (GET, POST, …), targetURL is the full URL of the
	// pod endpoint on the worker, headers are optional extra request headers,
	// and body is the request body (may be nil). Returns the HTTP status code,
	// response headers, and body.
	HTTPProxy(ctx context.Context, method, targetURL string, headers map[string]string, body []byte) (*types.HTTPProxyResponse, error)

	// WaitForInferenceServiceReady polls a KServe InferenceService until its
	// Ready condition is True or the context deadline is exceeded.
	// Non-OpenShift runtimes return nil immediately (no-op).
	WaitForInferenceServiceReady(ctx context.Context, isvcName string) error

	// Runtime type identification
	Type() types.RuntimeType
}
