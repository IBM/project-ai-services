// Package restore provides the catalog restore functionality.
// Currently only the Podman runtime is supported.
package restore

import (
	"fmt"

	restorePodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/restore/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Run dispatches the catalog restore to the appropriate runtime implementation.
func Run(runtime types.RuntimeType, backupFile string) error {
	switch runtime {
	case types.RuntimeTypePodman:
		return restorePodman.RestoreCatalog(backupFile)
	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("catalog restore is not yet supported for the OpenShift runtime")
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtime)
	}
}

// Made with Bob
