package openshift

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// OpenshiftBootstrap implements Bootstrap interface for Openshift runtime.
type OpenshiftBootstrap struct{}

// NewOpenshiftBootstrap creates a new Podman Openshift instance.
func NewOpenshiftBootstrap() *OpenshiftBootstrap {
	return &OpenshiftBootstrap{}
}

// Type returns the runtime type.
func (o *OpenshiftBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}
