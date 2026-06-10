package podman

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Backup creates a backup of application data.
func (p *PodmanApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	logger.Infof("Starting backup for application: %s\n", opts.Name, 0)
	logger.Infof("Target: %s\n", opts.Target, 0)

	// Validate target
	switch opts.Target {
	case "opensearch":
		return p.backupOpenSearch(ctx, opts.Name, opts.BackupFile)
	case "digitize":
		return fmt.Errorf("digitize backup is not yet supported")
	default:
		return fmt.Errorf("unsupported backup target: %s", opts.Target)
	}
}

// backupOpenSearch performs OpenSearch backup using a sidecar container.
func (p *PodmanApplication) backupOpenSearch(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Backing up OpenSearch data for application: %s\n", appName, 0)
	logger.Infof("OpenSearch Backup (Sidecar Container Approach)\n", 0)

	// Get application details from catalog API
	appDetails, err := cliUtils.GetAppDetailsWithComponents(appName)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID, 0)

	// Get component ID for opensearch
	componentID, err := cliUtils.GetComponentID(appDetails, "opensearch")
	if err != nil {
		return fmt.Errorf("failed to get component ID: %w", err)
	}
	logger.Infof("Component ID: %s\n", componentID, 0)

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

	logger.Infof("Container: %s\n", containerName, 0)
	logger.Infof("Pod ID: %s\n", podID, 0)

	// Perform backup using the backup package
	if err := backup.BackupOpenSearch(podmanCtx, podID, absBackupFile); err != nil {
		return err
	}

	logger.Infof("✅ Backup completed successfully: %s\n", absBackupFile, 0)

	return nil
}

// Made with Bob
