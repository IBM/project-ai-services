package podman

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Backup creates a backup of application data.
func (p *PodmanApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	logger.Infof("Starting backup for application: %s\n", opts.Name)
	logger.Infof("Target: %s\n", opts.Target)

	// Validate target
	switch opts.Target {
	case "opensearch":
		return p.backupOpenSearch(ctx, opts.Name, opts.BackupFile)
	case "digitize":
		return p.backupDigitize(ctx, opts.Name, opts.BackupFile)
	default:
		return fmt.Errorf("unsupported backup target: %s", opts.Target)
	}
}

// backupOpenSearch performs OpenSearch backup using a sidecar container.
func (p *PodmanApplication) backupOpenSearch(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Backing up OpenSearch data for application: %s\n", appName)
	logger.Infoln("OpenSearch Backup (Sidecar Container Approach)")

	// Get application details from catalog API
	appDetails, err := cliUtils.GetAppDetailsWithComponents(appName)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Get component ID for opensearch
	componentID, err := cliUtils.GetComponentID(appDetails, "opensearch")
	if err != nil {
		return fmt.Errorf("failed to get component ID: %w", err)
	}
	logger.Infof("Component ID: %s\n", componentID)

	// Generate backup filename if not provided
	if backupFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		backupFile = fmt.Sprintf("%s_opensearch_backup_%s.tar.gz", appName, timestamp)
	}

	// Ensure .tar.gz extension
	if !strings.HasSuffix(backupFile, ".tar.gz") {
		backupFile += ".tar.gz"
	}

	// Get absolute path for backup file
	absBackupFile, err := filepath.Abs(backupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

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
	if err := backup.BackupOpenSearch(podmanCtx, podID, absBackupFile); err != nil {
		return err
	}

	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

	return nil
}

func (p *PodmanApplication) backupDigitize(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Backing up digitize metadata for application: %s\n", appName)
	logger.Infoln("Digitize Export (API-based Approach)")

	appDetails, err := cliUtils.GetAppDetailsWithComponents(appName)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Generate backup filename if not provided
	absBackupFile, err := commonBackup.GetBackupFile(backupFile, appName)
	if err != nil {
		return err
	}

	digitizeURL, err := restore.GetDigitizeAPIURL(appDetails)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL)

	// Create digitize backup client and call Export API
	client := commonBackup.NewDigitizeBackupClient(digitizeURL)
	exportResponse, err := client.CallExportAPI()
	if err != nil {
		return err
	}

	if err := commonBackup.CreateDigitizeBackupArchive(absBackupFile, exportResponse); err != nil {
		return err
	}

	logDigitizeBackupSummary(exportResponse)
	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

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

	logger.Infof("  Returned records: %d\n", exportResponse.Pagination.ReturnedRecords)
}

var _ *catalogTypes.Application

// Made with Bob
