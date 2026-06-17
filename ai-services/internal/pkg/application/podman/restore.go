package podman

import (
	"context"
	"fmt"
	"path/filepath"

	commonrestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores application data from a backup file for Podman runtime.
func (p *PodmanApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name)
	logger.Infof("Target: %s\n", opts.Target)
	logger.Infof("Backup file: %s\n", opts.BackupFile)

	// Get application details from catalog API using existing utility
	appDetails, err := cliUtils.GetAppDetailsWithComponents(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		// Get component ID for opensearch
		componentID, err := cliUtils.GetComponentID(appDetails, opts.Target)
		if err != nil {
			return fmt.Errorf("failed to get component ID: %w", err)
		}
		logger.Infof("Component ID: %s\n", componentID)

		return p.restoreOpenSearch(ctx, componentID, absFilename)
	case "digitize":
		return p.restoreDigitize(ctx, appDetails, absFilename)
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}

// restoreOpenSearch restores OpenSearch data using podman sidecar approach.
func (p *PodmanApplication) restoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Call the OpenSearch-specific restore function
	return restore.RestoreOpenSearch(podmanCtx, templateID, backupFile)
}

// restoreDigitize restores digitize metadata using the Import API.
func (p *PodmanApplication) restoreDigitize(ctx context.Context, appDetails *catalogTypes.Application, backupFile string) error {
	logger.Infoln("Restoring digitize metadata")
	logger.Infoln("Digitize Import (API-based Approach)")

	importPayload, err := commonrestore.GetDigitizeData(backupFile)
	if err != nil {
		return err
	}

	// Get digitize service API URL from application details
	digitizeURL, err := restore.GetDigitizeAPIURL(appDetails)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL)

	// Create digitize restore client and call Import API
	client := commonrestore.NewDigitizeRestoreClient(digitizeURL)
	if err := client.CallImportAPI(importPayload); err != nil {
		return err
	}

	logger.Infoln("✓ Digitize metadata restore completed successfully")

	return nil
}
