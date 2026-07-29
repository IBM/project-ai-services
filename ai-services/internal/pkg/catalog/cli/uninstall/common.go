package uninstall

import (
	"context"
	"fmt"

	catalogOpenshift "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/openshift"
	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/podman"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Uninstall removes the catalog service and cleans up resources.
func Uninstall(opts cliutils.UninstallOptions) error {
	ctx := context.Background()

	// Remove catalog service based on runtime
	switch opts.Runtime {
	case types.RuntimeTypePodman:
		return catalogPodman.UninstallCatalog(ctx, opts)

	case types.RuntimeTypeOpenShift:
		return catalogOpenshift.UninstallCatalog(ctx, opts)

	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.Runtime)
	}
}
