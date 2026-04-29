package uninstall

import (
	"context"
	"fmt"

	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// UninstallOptions contains the configuration for uninstalling the catalog service.
type UninstallOptions struct {
	Runtime types.RuntimeType
	AutoYes bool
}

// Uninstall removes the catalog service and cleans up resources.
func Uninstall(opts UninstallOptions) error {
	ctx := context.Background()

	// Remove catalog service based on runtime
	switch opts.Runtime {
	case types.RuntimeTypePodman:
		return catalogPodman.UninstallCatalog(ctx, opts.AutoYes)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog uninstall")

	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.Runtime)
	}
}
