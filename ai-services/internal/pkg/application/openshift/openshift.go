package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// OpenshiftApplication implements the Application interface for Openshift runtime.
type OpenshiftApplication struct {
	runtime runtime.Runtime
}

// NewOpenshiftApplication creates a new OpenshiftApplication instance.
func NewOpenshiftApplication(runtimeClient runtime.Runtime) *OpenshiftApplication {
	return &OpenshiftApplication{
		runtime: runtimeClient,
	}
}

// Type returns the runtime type.
func (o *OpenshiftApplication) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}
