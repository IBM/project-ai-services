package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// BootstrapFactory creates bootstrap instances based on configuration.
type BootstrapFactory struct {
	runtimeType types.RuntimeType
}

// NewBootstrapFactory creates a new bootstrap factory with the specified runtime type.
func NewBootstrapFactory(runtimeType types.RuntimeType) *BootstrapFactory {
	return &BootstrapFactory{
		runtimeType: runtimeType,
	}
}

// Create creates a bootstrap instance based on the factory configuration.
func (f *BootstrapFactory) Create() (Bootstrap, error) {
	switch f.runtimeType {
	case types.RuntimeTypePodman:
		logger.Infof("Initializing Podman bootstrap\n", logger.VerbosityLevelDebug)

		return podman.NewPodmanBootstrap(), nil

	case types.RuntimeTypeOpenShift:
		logger.Infof("Initializing OpenShift bootstrap\n", logger.VerbosityLevelDebug)

		return openshift.NewOpenshiftBootstrap(), nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", f.runtimeType)
	}
}

// Made with Bob
