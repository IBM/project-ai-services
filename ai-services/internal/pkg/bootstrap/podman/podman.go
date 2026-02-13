package podman

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// PodmanBootstrap implements Bootstrap interface for Podman runtime.
type PodmanBootstrap struct{}

// NewPodmanBootstrap creates a new Podman bootstrap instance.
func NewPodmanBootstrap() *PodmanBootstrap {
	return &PodmanBootstrap{}
}

// Type returns the runtime type.
func (p *PodmanBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypePodman
}
