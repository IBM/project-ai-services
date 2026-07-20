// Package podman implements the catalog restore operation for the Podman runtime.
// It restores:
//  1. PostgreSQL database (psql via exec into the running postgres container)
//  2. Caddy autosave.json               (<BaseDir>/common/caddy-config/caddy/autosave.json)
//  3. User-supplied TLS certs           (<BaseDir>/common/caddy/certs/tls-*.crt/.key)
//     OR Caddy self-signed PKI          (<BaseDir>/common/caddy/pki/ + certificates/)
//
// Restore sequence:
//  1. Extract and validate the backup archive.
//  2. Restore the PostgreSQL database (pod still running – postgres container must be up).
//  3. Stop the Caddy pod.
//  4. Restore Caddy config files.
//  5. Restart the Caddy pod (loads restored autosave.json with application routes via --resume).
//
// Postgres is restored first (while running) because podman exec cannot target a
// stopped container; the restore would silently fail otherwise.
package podman

import (
	"fmt"
	"os"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// RestoreCatalog is the top-level entry point for restoring all catalog infrastructure data
// from a tar.gz archive created by BackupCatalog.
func RestoreCatalog(backupFile string) error {
	// Validate that the backup file exists before touching any live state.
	if !utils.FileExists(backupFile) {
		return fmt.Errorf("backup file not found: %s", backupFile)
	}

	pc, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Retrieve running catalog pod config (BaseDir, domain, https port).
	catalogConfig, _, err := catalogUtils.GetCatalogPodConfig(pc)
	if err != nil {
		return fmt.Errorf("catalog service is not running – cannot restore: %w", err)
	}

	logger.Infof("Starting catalog restore from %s\n", backupFile)

	tempDir, err := extractAndValidateArchive(backupFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Warningf("failed to remove temporary directory %s: %v\n", tempDir, err)
		}
	}()

	if err := runFullRestoreSequence(pc, catalogConfig, tempDir); err != nil {
		return err
	}

	commonBackup.LogArchiveSize(backupFile)
	logger.Infoln("✅ Catalog restore completed successfully")

	return nil
}

// extractAndValidateArchive creates a temp dir, extracts the archive into it,
// validates the expected structure, and returns the temp dir path.
func extractAndValidateArchive(backupFile string) (string, error) {
	tempDir, err := os.MkdirTemp("", "catalog-restore-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary working directory: %w", err)
	}

	logger.Infoln("Extracting backup archive...")

	if err := utils.ExtractTarGz(backupFile, tempDir); err != nil {
		_ = os.RemoveAll(tempDir)

		return "", fmt.Errorf("failed to extract backup archive: %w", err)
	}

	if err := validateBackupContents(tempDir); err != nil {
		_ = os.RemoveAll(tempDir)

		return "", err
	}

	return tempDir, nil
}

// runFullRestoreSequence executes the complete restore sequence:
//
//  1. Restore the database while the pods are still running (postgres container must be up).
//  2. Stop the Caddy pod.
//  3. Restore Caddy config files.
//  4. Restart the Caddy pod (loads restored autosave.json with application routes via --resume).
func runFullRestoreSequence(pc *podman.PodmanClient, catalogConfig *catalogUtils.PodmanConfigureOptions, tempDir string) error {
	// Step 1 – restore postgres while the container is running.
	logger.Infoln("Restore step: postgres")

	if err := restorePostgres(pc.Context, tempDir); err != nil {
		return fmt.Errorf("restore step %q failed: %w", "postgres", err)
	}

	// Step 2 – stop Caddy pod.
	logger.Infof("Stopping Caddy pod %s...\n", catalogpodman.CaddyPodName)

	if err := pc.StopPod(catalogpodman.CaddyPodName); err != nil {
		return fmt.Errorf("failed to stop Caddy pod: %w", err)
	}

	// Step 3 – restore Caddy config files while Caddy is down.
	logger.Infoln("Restore step: caddy")

	if err := restoreCaddyFiles(catalogConfig.BaseDir, tempDir); err != nil {
		return fmt.Errorf("restore step %q failed: %w", "caddy", err)
	}

	// Step 4 – restart Caddy so it picks up the restored autosave.json.
	logger.Infof("Starting Caddy pod %s...\n", catalogpodman.CaddyPodName)

	if err := pc.StartPod(catalogpodman.CaddyPodName); err != nil {
		return fmt.Errorf("failed to restart Caddy pod: %w", err)
	}

	return nil
}

// validateBackupContents checks that the expected top-level directories are present
// in the extracted archive.
func validateBackupContents(tempDir string) error {
	required := []string{catalogpodman.DirDB, catalogpodman.DirCaddy}

	for _, d := range required {
		path := tempDir + "/" + d
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("invalid backup archive: missing required directory %q", d)
		}
	}

	return nil
}
