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

// RuntimeFactory creates runtime instances based on configuration.
type RuntimeFactory struct {
	runtimeType types.RuntimeType
}

// NewRuntimeFactory creates a new runtime factory with the specified runtime type.
func NewRuntimeFactory(runtimeType types.RuntimeType) *RuntimeFactory {
	return &RuntimeFactory{
		runtimeType: runtimeType,
	}
}

// NewFactoryFromEnv creates a factory using environment variable or default.
func NewFactoryFromEnv() *RuntimeFactory {
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

	return NewRuntimeFactory(runtimeType)
}

// Create creates a runtime instance based on the factory configuration.
func (f *RuntimeFactory) Create() (Runtime, error) {
	return CreateRuntime(f.runtimeType)
}

// GetRuntimeType returns the configured runtime type.
func (f *RuntimeFactory) GetRuntimeType() types.RuntimeType {
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

	case types.RuntimeTypeOpenShift:
		logger.Infof("Initializing OpenShift runtime\n", logger.VerbosityLevelDebug)
		client, err := openshift.NewOpenshiftClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenShift client: %w", err)
		}

		return client, nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
