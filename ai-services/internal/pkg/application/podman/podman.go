package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// PodmanApplication implements the Application interface for Podman runtime.
type PodmanApplication struct {
	runtime runtime.Runtime
}

// NewPodmanApplication creates a new PodmanApplication instance.
func NewPodmanApplication(runtimeClient runtime.Runtime) *PodmanApplication {
	return &PodmanApplication{
		runtime: runtimeClient,
	}
}

// Type returns the runtime type.
func (p *PodmanApplication) Type() types.RuntimeType {
	return types.RuntimeTypePodman
}
