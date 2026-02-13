package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

const (
	// EnvBootstrapRuntime is the environment variable for bootstrap runtime type.
	EnvBootstrapRuntime = "AI_SERVICES_RUNTIME"
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

// NewFactoryFromEnv creates a factory using environment variable or default.
func NewFactoryFromEnv() *BootstrapFactory {
	runtimeType := types.RuntimeTypePodman // default
	if envRuntime := os.Getenv(EnvBootstrapRuntime); envRuntime != "" {
		rt := types.RuntimeType(strings.ToLower(envRuntime))
		if rt.Valid() {
			runtimeType = rt
		} else {
			logger.Warningf("Invalid runtime type in %s: %s, using default: %s\n",
				EnvBootstrapRuntime, envRuntime, types.RuntimeTypePodman)
		}
	}

	return NewBootstrapFactory(runtimeType)
}

// Create creates a bootstrap instance based on the factory configuration.
func (f *BootstrapFactory) Create() (Bootstrap, error) {
	return CreateBootstrap(f.runtimeType)
}

// GetRuntimeType returns the configured runtime type.
func (f *BootstrapFactory) GetRuntimeType() types.RuntimeType {
	return f.runtimeType
}

// CreateBootstrap creates a bootstrap instance based on the specified type.
func CreateBootstrap(runtimeType types.RuntimeType) (Bootstrap, error) {
	switch runtimeType {
	case types.RuntimeTypePodman:
		logger.Infof("Initializing Podman bootstrap\n", logger.VerbosityLevelDebug)

		return podman.NewPodmanBootstrap(), nil

	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
