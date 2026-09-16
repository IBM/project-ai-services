package runtime

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// RuntimeFactory creates runtime instances based on configuration.
type RuntimeFactory struct {
	runtimeType types.RuntimeType
}

// NewRuntimeFactory creates a new runtime factory with the specified runtime type.
func NewRuntimeFactory(runtimeType types.RuntimeType) *RuntimeFactory {
	return &RuntimeFactory{
		runtimeType: runtimeType,
	}
}

// Create creates a local runtime instance based on the factory configuration.
func (f *RuntimeFactory) Create(namespace string) (Runtime, error) {
	return CreateRuntime(f.runtimeType, namespace)
}

// CreateRemote returns a RemoteRuntime that forwards calls to the named worker
// over the gRPC CommandStream. The worker's runtime type is looked up from the
// registry (stored there at Register time) and used only for Type() reporting —
// the gRPC protocol itself is runtime-agnostic.
// For OpenShift workers, namespace is applied so that namespace-scoped operations
// (ListPods, ListRoutes, DeletePVCs…) target the correct application namespace.
// For Podman workers namespace is ignored.
// Returns an error if the worker is not currently connected.
func (f *RuntimeFactory) CreateRemote(workerName string, reg stream.WorkerRegistry, namespace string) (Runtime, error) {
	rtStr, ok := reg.WorkerRuntimeType(workerName)
	if !ok {
		return nil, fmt.Errorf("worker %s is not connected", workerName)
	}

	rt := remote.New(workerName, types.RuntimeType(rtStr), reg)
	if f.runtimeType == types.RuntimeTypeOpenShift && namespace != "" {
		return rt.WithNamespace(namespace), nil
	}

	return rt, nil
}

// GetRuntimeType returns the configured runtime type.
func (f *RuntimeFactory) GetRuntimeType() types.RuntimeType {
	return f.runtimeType
}

// CreateRuntime creates a runtime instance based on the specified type.
func CreateRuntime(runtimeType types.RuntimeType, namespace string) (Runtime, error) {
	switch runtimeType {
	case types.RuntimeTypePodman:
		logger.Debugf("Initializing Podman runtime\n")
		client, err := podman.NewPodmanClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create Podman client: %w", err)
		}

		return client, nil

	case types.RuntimeTypeOpenShift:
		logger.Debugf("Initializing OpenShift runtime\n")
		client, err := openshift.NewOpenshiftClientWithNamespace(namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenShift client: %w", err)
		}

		return client, nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
