package openshift

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	commonrestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores application data from a backup file for OpenShift runtime.
func (o *OpenshiftApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name)
	logger.Infof("Target: %s\n", opts.Target)
	logger.Infof("Backup file: %s\n", opts.BackupFile)

	// Get application details from catalog API
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

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		// Get component ID (templateID) — used as ai-services.io/template label selector
		templateID, err := cliUtils.GetComponentID(appDetails, "opensearch")
		if err != nil {
			return fmt.Errorf("failed to get opensearch component ID: %w", err)
		}
		logger.Infof("OpenSearch component ID: %s\n", templateID)

		// Derive namespace: "ai-services-<first 8 chars of app UUID>"
		appUUID, err := uuid.Parse(appDetails.ID)
		if err != nil {
			return fmt.Errorf("failed to parse application UUID: %w", err)
		}
		namespace := catalogUtils.AppNamespace(appUUID)
		logger.Infof("Namespace: %s\n", namespace)

		return restore.RestoreOpenSearch(ctx, templateID, namespace, absFilename)
	case "digitize":
		return o.restoreDigitize(ctx, appDetails, absFilename)
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}

// restoreDigitize restores digitize metadata using the Import API for OpenShift.
func (o *OpenshiftApplication) restoreDigitize(ctx context.Context, appDetails *catalogTypes.Application, backupFile string) error {
	logger.Infof("Restoring digitize metadata\n")
	logger.Infof("Digitize Import (API-based Approach)\n")

	importPayload, err := commonrestore.GetDigitizeData(backupFile)
	if err != nil {
		return err
	}

	// Get digitize service API URL from catalog service endpoints
	digitizeURL, err := commonrestore.GetDigitizeAPIURL(appDetails)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL)

	// Create digitize restore client and call Import API
	client := commonrestore.NewDigitizeRestoreClient(digitizeURL)
	if err := client.CallImportAPI(ctx, importPayload); err != nil {
		return err
	}

	logger.Infof("✓ Digitize metadata restore completed successfully\n")

	return nil
}

// Made with Bob
