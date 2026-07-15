package configure

import (
	"context"
	"fmt"

	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// ConfigureOptions contains the configuration for the catalog configure command.
type ConfigureOptions struct {
	Runtime types.RuntimeType
	Podman  catalogPodman.PodmanConfigureOptions
}

// Run executes the configure process for the catalog service.
// It creates runtime-specific options and calls the appropriate runtime implementation.
func Run(opts ConfigureOptions) error {
	ctx := context.Background()
	// Deploy catalog service based on runtime
	switch opts.Runtime {
	case types.RuntimeTypePodman:
		return catalogPodman.DeployCatalog(ctx, opts.Podman)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog configure")

	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.Runtime)
	}
}

// Made with Bob
