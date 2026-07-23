package info

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/info/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/info/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Run displays catalog service information based on the runtime type.
func Run(ctx context.Context, runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman:
		return podman.DisplayCatalogInfo()
	case types.RuntimeTypeOpenShift:
		return openshift.DisplayCatalogInfo(ctx)
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
