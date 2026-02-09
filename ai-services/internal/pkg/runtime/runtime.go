package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

const (
	// EnvRuntimeType is the environment variable for runtime type.
	EnvRuntimeType = "AI_SERVICES_RUNTIME"
)

// Factory creates runtime instances based on configuration.
type Factory struct {
	runtimeType types.RuntimeType
}

// NewFactory creates a new runtime factory with the specified runtime type.
func NewFactory(runtimeType types.RuntimeType) *Factory {
	return &Factory{
		runtimeType: runtimeType,
	}
}

// NewFactoryFromEnv creates a factory using environment variable or default.
func NewFactoryFromEnv() *Factory {
	runtimeType := types.RuntimeTypePodman // default
	if envRuntime := os.Getenv(EnvRuntimeType); envRuntime != "" {
		rt := types.RuntimeType(strings.ToLower(envRuntime))
		if rt.Valid() {
			runtimeType = rt
		} else {
			logger.Warningf("Invalid runtime type in %s: %s, using default: %s\n",
				EnvRuntimeType, envRuntime, types.RuntimeTypePodman)
		}
	}

	return NewFactory(runtimeType)
}

// Create creates a runtime instance based on the factory configuration.
func (f *Factory) Create() (Runtime, error) {
	return CreateRuntime(f.runtimeType)
}

// GetRuntimeType returns the configured runtime type.
func (f *Factory) GetRuntimeType() types.RuntimeType {
	return f.runtimeType
}

// CreateRuntime creates a runtime instance based on the specified type.
func CreateRuntime(runtimeType types.RuntimeType) (Runtime, error) {
	switch runtimeType {
	case types.RuntimeTypePodman:
		logger.Infof("Initializing Podman runtime\n", logger.VerbosityLevelDebug)
		client, err := podman.NewPodmanClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create Podman client: %w", err)
		}

		return client, nil

	case types.RuntimeTypeOpenshift:
		logger.Infof("Initializing Openshift runtime\n", logger.VerbosityLevelDebug)
		client, err := openshift.NewOpenshiftClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create Openshift client: %w", err)
		}

		return client, nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// CreateRuntimeWithNamespace creates a runtime with a specific namespace (for Openshift).
func CreateRuntimeWithNamespace(runtimeType types.RuntimeType, namespace string) (Runtime, error) {
	switch runtimeType {
	case types.RuntimeTypePodman:
		// Podman doesn't use namespaces in the same way
		return CreateRuntime(runtimeType)

	case types.RuntimeTypeOpenshift:
		logger.Infof("Initializing Openshift runtime with namespace: %s\n", namespace, logger.VerbosityLevelDebug)
		client, err := openshift.NewOpenshiftClientWithNamespace(namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to create Openshift client: %w", err)
		}

		return client, nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
