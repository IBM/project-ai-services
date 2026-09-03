package utils

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// UninstallOptions contains the configuration for uninstalling the catalog service.
type UninstallOptions struct {
	Runtime     types.RuntimeType
	AutoYes     bool
	SkipCleanup bool
}
