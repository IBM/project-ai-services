package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimePodman "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
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

// getPodmanContext extracts the Podman context from the runtime client.
func (p *PodmanApplication) getPodmanContext() (context.Context, error) {
	podmanClient, ok := p.runtime.(*runtimePodman.PodmanClient)
	if !ok {
		return nil, fmt.Errorf("runtime is not a Podman client")
	}

	return podmanClient.Context, nil
}
