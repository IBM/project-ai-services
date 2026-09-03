package utils

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// UninstallOptions carries the parameters needed to uninstall a worker node.
type UninstallOptions struct {
	// RuntimeType is the local runtime (podman / openshift).
	RuntimeType types.RuntimeType

	// AutoYes skips the interactive confirmation prompt.
	AutoYes bool
}
