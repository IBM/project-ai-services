package openshift

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// skipUpgrade controls whether existing operator subscriptions are silently
// skipped instead of being offered an upgrade prompt.
var skipUpgrade bool

// SetSkipUpgrade enables or disables skipping the operator upgrade prompt flow.
func SetSkipUpgrade(skip bool) {
	skipUpgrade = skip
}

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
