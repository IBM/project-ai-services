package podman

import (
	"context"
	"fmt"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	commonRestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Backup creates a single backup archive containing both OpenSearch and digitize data.
func (p *PodmanApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	logger.Infof("Starting backup for application: %s\n", opts.Name)

	appDetails, err := cliUtils.GetAppDetailsWithComponents(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Generate backup filename if not provided
	absBackupFile, err := commonBackup.GetBackupFile(opts.BackupFile, opts.Name)
	if err != nil {
		return err
	}

	// Both parts are staged here and packed into one archive at the end.
	stageDir, cleanup, err := commonBackup.NewStagingDir()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := p.backupOpenSearch(ctx, appDetails, stageDir); err != nil {
		return err
	}

	if err := p.backupDigitize(ctx, appDetails, stageDir); err != nil {
		return err
	}

	if err := commonBackup.CreateBackupArchive(stageDir, absBackupFile); err != nil {
		return err
	}

	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

	return nil
}

// backupOpenSearch performs OpenSearch backup using a sidecar container into stageDir.
func (p *PodmanApplication) backupOpenSearch(ctx context.Context, appDetails *catalogTypes.Application, stageDir string) error {
	logger.Infof("Backing up OpenSearch data for application: %s\n", appDetails.Name)
	logger.Infoln("OpenSearch Backup (Sidecar Container Approach)")

	// Get component ID for opensearch
	componentID, err := cliUtils.GetComponentID(appDetails, "opensearch")
	if err != nil {
		return fmt.Errorf("failed to get component ID: %w", err)
	}
	logger.Infof("Component ID: %s\n", componentID)

	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Find OpenSearch container and get pod ID using component ID
	containerName, podID, err := common.FindContainerAndPod(podmanCtx, componentID)
	if err != nil {
		return err
	}

	logger.Infof("Container: %s\n", containerName)
	logger.Infof("Pod ID: %s\n", podID)

	// Perform backup using the backup package
	return backup.BackupOpenSearch(podmanCtx, podID, stageDir)
}

// backupDigitize exports digitize metadata via the Export API into stageDir.
func (p *PodmanApplication) backupDigitize(ctx context.Context, appDetails *catalogTypes.Application, stageDir string) error {
	logger.Infof("Backing up digitize metadata for application: %s\n", appDetails.Name)
	logger.Infoln("Digitize Export (API-based Approach)")

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

	logDigitizeBackupSummary(exportResponse)

	return nil
}

func logDigitizeBackupSummary(exportResponse *commonBackup.DigitizeExportResponse) {
	if exportResponse == nil {
		return
	}

	logger.Infoln("Export summary:")

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
