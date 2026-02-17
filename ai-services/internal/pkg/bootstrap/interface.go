package bootstrap

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// Bootstrap defines the interface for environment bootstrapping operations.
// Different runtimes implement this interface to provide
// runtime-specific bootstrap functionality.
type Bootstrap interface {
	// Configure performs the complete configuration of the environment.
	// This includes installing dependencies, configuring runtime, and setting up hardware.
	Configure() error

	// Validate runs all validation checks to ensure the environment is properly configured.
	// Returns an error if any validation fails.
	Validate(skip map[string]bool) error

	// Type returns the runtime type this bootstrap implementation supports.
	Type() types.RuntimeType
}

// Made with Bob
