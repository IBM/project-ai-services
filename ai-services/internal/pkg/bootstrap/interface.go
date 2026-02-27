package bootstrap

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// Bootstrap defines the interface for environment bootstrapping operations.
// Different runtimes implement this interface to provide
// runtime-specific bootstrap functionality.
type Bootstrap interface {
	// Configure performs the complete configuration of the environment.
	// This includes installing dependencies, configuring runtime, and setting up hardware.
	Configure() error

	// Type returns the runtime type this bootstrap implementation supports.
	Type() types.RuntimeType
}

// Made with Bob
