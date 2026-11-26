package podman

import (
	"context"
	"fmt"
	"path/filepath"

	commonrestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	podmanrestore "github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores OpenSearch and digitize data from a backup file for Podman runtime.
// Older single-part archives are supported: only the parts present in the archive are restored.
func (p *PodmanApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name)
	logger.Infof("Backup file: %s\n", opts.BackupFile)

	// Get application details from catalog API using existing utility
	appDetails, err := cliUtils.GetAppDetailsWithComponents(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	rootDir, cleanup, err := podmanrestore.ExtractBackup(absFilename)
	if err != nil {
		return err
	}
	defer cleanup()

	opensearchDir, digitizeDir, err := podmanrestore.LocateBackupParts(rootDir)
	if err != nil {
		return err
	}

	if opensearchDir != "" {
		componentID, err := cliUtils.GetComponentID(appDetails, "opensearch")
		if err != nil {
			return fmt.Errorf("failed to get component ID: %w", err)
		}
		logger.Infof("Component ID: %s\n", componentID)

		if err := p.restoreOpenSearch(ctx, componentID, opensearchDir); err != nil {
			return fmt.Errorf("opensearch restore failed: %w", err)
		}
	}

	if digitizeDir != "" {
		if err := commonrestore.RestoreDigitize(ctx, appDetails, digitizeDir); err != nil {
			return fmt.Errorf("digitize restore failed: %w", err)
		}
	}

	return nil
}

// restoreOpenSearch restores OpenSearch data using podman sidecar approach.
func (p *PodmanApplication) restoreOpenSearch(ctx context.Context, templateID, backupOSDir string) error {
	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Call the OpenSearch-specific restore function
	return podmanrestore.RestoreOpenSearch(podmanCtx, templateID, backupOSDir)
}
