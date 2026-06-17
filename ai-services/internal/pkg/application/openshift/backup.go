package openshift

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Backup creates a backup of application data for OpenShift runtime.
func (o *OpenshiftApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	logger.Infof("Starting backup for application: %s\n", opts.Name)
	logger.Infof("Target: %s\n", opts.Target)

	// Execute backup based on target
	switch opts.Target {
	case "opensearch":
		return o.backupOpenSearch(ctx, opts.Name, opts.BackupFile)
	case "digitize":
		return o.backupDigitize(ctx, opts.Name, opts.BackupFile)
	default:
		return fmt.Errorf("unsupported target for OpenShift: %s", opts.Target)
	}
}

// backupOpenSearch performs OpenSearch backup using a sidecar pod.
func (o *OpenshiftApplication) backupOpenSearch(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Backing up OpenSearch data for application: %s\n", appName)

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

	// Perform backup using the backup package
	if err := backup.BackupOpenSearch(ctx, appName, absBackupFile); err != nil {
		return err
	}

	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

	return nil
}

// backupDigitize backs up digitize metadata using the Export API for OpenShift.
func (o *OpenshiftApplication) backupDigitize(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Backing up digitize metadata\n")
	logger.Infof("Digitize Export (API-based Approach)\n")

	// Generate backup filename if not provided
	absBackupFile, err := commonBackup.GetBackupFile(backupFile, appName)
	if err != nil {
		return err
	}

	// Get digitize service API URL from OpenShift routes
	digitizeURL, err := o.getDigitizeAPIURL(ctx, appName)
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

	// Create backup archive using shared function
	if err := commonBackup.CreateDigitizeBackupArchive(absBackupFile, exportResponse); err != nil {
		return err
	}

	// Log backup summary
	logDigitizeBackupSummary(exportResponse)
	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile)

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

	logger.Infof("  Returned records: %d\n", exportResponse.Pagination.ReturnedRecords)
}

// Made with Bob
