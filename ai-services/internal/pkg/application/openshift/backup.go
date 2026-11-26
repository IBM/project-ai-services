package openshift

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	commonRestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Backup creates a single backup archive containing both OpenSearch and digitize data for OpenShift runtime.
func (o *OpenshiftApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	logger.Infof("Starting backup for application: %s\n", opts.Name)

	// Get application details from catalog API
	appDetails, err := cliUtils.GetAppDetailsWithComponents(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Generate backup filename if not provided
	absBackupFile, err := commonBackup.GetBackupFile(opts.BackupFile, appDetails.Name)
	if err != nil {
		return err
	}

	// Both parts are staged here and packed into one archive at the end.
	stageDir, cleanup, err := commonBackup.NewStagingDir()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := o.backupOpenSearch(ctx, appDetails, stageDir); err != nil {
		return err
	}

	if err := o.backupDigitize(ctx, appDetails, stageDir); err != nil {
		return err
	}

	if err := commonBackup.CreateBackupArchive(stageDir, absBackupFile); err != nil {
		return err
	}

	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

	return nil
}

// backupOpenSearch performs OpenSearch backup using a sidecar pod into stageDir.
func (o *OpenshiftApplication) backupOpenSearch(ctx context.Context, appDetails *catalogTypes.Application, stageDir string) error {
	logger.Infof("Backing up OpenSearch data for application: %s\n", appDetails.Name)

	// Get component ID for opensearch (vectordb) — this is the templateID used as pod label
	templateID, err := cliUtils.GetComponentID(appDetails, "opensearch")
	if err != nil {
		return fmt.Errorf("failed to get opensearch component ID: %w", err)
	}
	logger.Infof("OpenSearch component ID: %s\n", templateID)

	// Derive namespace using catalog convention: "ai-services-<first 8 chars of app UUID>"
	appUUID, err := uuid.Parse(appDetails.ID)
	if err != nil {
		return fmt.Errorf("failed to parse application UUID: %w", err)
	}
	namespace := catalogUtils.AppNamespace(appUUID)
	logger.Infof("Namespace: %s\n", namespace)

	return backup.BackupOpenSearch(ctx, templateID, namespace, stageDir)
}

// backupDigitize exports digitize metadata using the Export API into stageDir for OpenShift.
func (o *OpenshiftApplication) backupDigitize(ctx context.Context, appDetails *catalogTypes.Application, stageDir string) error {
	logger.Infof("Backing up digitize metadata\n")
	logger.Infof("Digitize Export (API-based Approach)\n")

	// Get digitize service API URL from catalog service endpoints
	digitizeURL, err := commonRestore.GetDigitizeAPIURL(appDetails)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL)

	// Create digitize backup client and call Export API
	client := commonBackup.NewDigitizeBackupClient(digitizeURL)

	exportResponse, err := client.CallExportAPI(ctx)
	if err != nil {
		return err
	}

	if err := commonBackup.WriteDigitizeBackup(stageDir, exportResponse); err != nil {
		return err
	}

	// Log backup summary
	logDigitizeBackupSummary(exportResponse)

	return nil
}

// logDigitizeBackupSummary logs the backup summary from the export response.
func logDigitizeBackupSummary(exportResponse *commonBackup.DigitizeExportResponse) {
	if exportResponse == nil {
		return
	}

	logger.Infof("Export summary:\n")

	if exportResponse.Summary.Jobs.TotalExported > 0 || exportResponse.Summary.Jobs.Completed > 0 || exportResponse.Summary.Jobs.Failed > 0 {
		logger.Infof("  Jobs - exported: %d, completed: %d, failed: %d\n",
			exportResponse.Summary.Jobs.TotalExported,
			exportResponse.Summary.Jobs.Completed,
			exportResponse.Summary.Jobs.Failed)
	}

	if exportResponse.Summary.Documents.TotalExported > 0 || exportResponse.Summary.Documents.Completed > 0 || exportResponse.Summary.Documents.Failed > 0 {
		logger.Infof("  Documents - exported: %d, completed: %d, failed: %d\n",
			exportResponse.Summary.Documents.TotalExported,
			exportResponse.Summary.Documents.Completed,
			exportResponse.Summary.Documents.Failed)
	}

	if exportResponse.Summary.DocumentChecksums.TotalExported > 0 {
		logger.Infof("  Document checksums - exported: %d\n", exportResponse.Summary.DocumentChecksums.TotalExported)
	}

	logger.Infof("  Returned records: %d\n", exportResponse.Pagination.ReturnedRecords)
}

// Made with Bob
