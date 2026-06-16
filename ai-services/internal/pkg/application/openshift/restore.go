package openshift

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores application data from a backup file for OpenShift runtime.
func (o *OpenshiftApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name, 0)
	logger.Infof("Target: %s\n", opts.Target, 0)
	logger.Infof("Backup file: %s\n", opts.BackupFile, 0)

	// For OpenShift, use the name as-is (namespace convention)
	applicationID := opts.Name

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		return restore.RestoreOpenSearch(ctx, applicationID, absFilename)
	case "digitize":
		return fmt.Errorf("restore for target 'digitize' is not yet implemented")
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}
