// Package backup provides the catalog backup functionality.
// Currently only the Podman runtime is supported.
package backup

import (
	"fmt"

	backupPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/backup/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Run dispatches the catalog backup to the appropriate runtime implementation.
func Run(runtime types.RuntimeType, backupFile string) error {
	switch runtime {
	case types.RuntimeTypePodman:
		return backupPodman.BackupCatalog(backupFile)
	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("catalog backup is not yet supported for the OpenShift runtime")
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtime)
	}
}

// Made with Bob
