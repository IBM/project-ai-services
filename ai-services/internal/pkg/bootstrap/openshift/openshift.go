package openshift

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// Option configures an OpenshiftBootstrap instance.
type Option func(*OpenshiftBootstrap)

// WithSkipUpgrade configures whether to skip the operator upgrade prompt flow.
func WithSkipUpgrade(skip bool) Option {
	return func(o *OpenshiftBootstrap) {
		o.skipUpgrade = skip
	}
}

// OpenshiftBootstrap implements Bootstrap interface for Openshift runtime.
type OpenshiftBootstrap struct {
	skipUpgrade bool
}

// NewOpenshiftBootstrap creates a new OpenshiftBootstrap instance with the provided options.
func NewOpenshiftBootstrap(opts ...Option) *OpenshiftBootstrap {
	b := &OpenshiftBootstrap{}
	for _, opt := range opts {
		opt(b)
	}

	return b
}

// Type returns the runtime type.
func (o *OpenshiftBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}
