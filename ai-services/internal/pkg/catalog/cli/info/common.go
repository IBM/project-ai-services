package info

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/info/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Run displays catalog service information based on the runtime type.
func Run(runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman:
		return podman.DisplayCatalogInfo()
	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("catalog info is not yet supported for OpenShift runtime")
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// Made with Bob
