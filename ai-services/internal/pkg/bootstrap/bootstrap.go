package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Option defines a functional option for configuring bootstrap.
type Option func(*options)

type options struct {
	skipUpgrade bool
}

// WithSkipUpgrade configures whether to skip the operator upgrade prompt flow.
func WithSkipUpgrade(skip bool) Option {
	return func(o *options) {
		o.skipUpgrade = skip
	}
}

// BootstrapFactory creates bootstrap instances based on configuration.
type BootstrapFactory struct {
	runtimeType types.RuntimeType
	opts        options
}

// NewBootstrapFactory creates a new bootstrap factory with the specified runtime type and options.
func NewBootstrapFactory(runtimeType types.RuntimeType, opts ...Option) *BootstrapFactory {
	var opt options
	for _, o := range opts {
		o(&opt)
	}

	return &BootstrapFactory{
		runtimeType: runtimeType,
		opts:        opt,
	}
}

// Create creates a bootstrap instance based on the factory configuration.
func (f *BootstrapFactory) Create() (Bootstrap, error) {
	switch f.runtimeType {
	case types.RuntimeTypePodman:
		logger.Debugf("Initializing Podman bootstrap\n")

		return podman.NewPodmanBootstrap(), nil

	case types.RuntimeTypeOpenShift:
		logger.Debugf("Initializing OpenShift bootstrap\n")

		return openshift.NewOpenshiftBootstrap(openshift.WithSkipUpgrade(f.opts.skipUpgrade)), nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", f.runtimeType)
	}
}
