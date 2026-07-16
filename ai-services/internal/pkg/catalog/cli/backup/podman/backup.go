// Package podman implements the catalog backup operation for the Podman runtime.
// It backs up:
//  1. PostgreSQL database (pg_dump via exec into the running postgres container)
//  2. Podman secret: catalog-secret     (admin password hash)
//  3. Caddy autosave.json               (<BaseDir>/common/caddy-config/caddy/autosave.json)
//  4. User-supplied TLS certs           (<BaseDir>/common/caddy/certs/tls-*.crt/.key)
//     OR Caddy self-signed PKI          (<BaseDir>/common/caddy/pki/ + certificates/)
//
// catalog-db-secret is intentionally excluded: the postgres user password lives in
// the database data-volume and must stay in sync with it across restore operations.
package podman

import (
	"context"
	"fmt"
	"os"
	"time"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// BackupCatalog is the top-level entry point for backing up all catalog infrastructure data.
// It creates a single tar.gz archive containing the database dump, secrets, and Caddy config files.
func BackupCatalog(backupFile string) error {
	pc, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Retrieve running catalog pod config (BaseDir, domain, https port).
	catalogConfig, _, err := catalogUtils.GetCatalogPodConfig(pc)
	if err != nil {
		return fmt.Errorf("catalog service is not running – cannot back up: %w", err)
	}

	backupFile = resolveBackupFile(backupFile)

	logger.Infof("Starting catalog backup → %s\n", backupFile)

	// Work inside a temp directory; only the final tar.gz is written to the
	// user-requested path, so no partial state is ever left on disk.
	tempDir, err := os.MkdirTemp("", "catalog-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary working directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Warningf("failed to remove temporary directory %s: %v\n", tempDir, err)
		}
	}()

	if err := runBackupSteps(pc.Context, pc, catalogConfig.BaseDir, tempDir); err != nil {
		return err
	}

	// Pack everything into the final archive.
	entries := []string{
		catalogpodman.DirDB, catalogpodman.DirSecrets, catalogpodman.DirCaddy,
	}
	if err := commonBackup.CreateTarGzArchive(tempDir, backupFile, entries); err != nil {
		return fmt.Errorf("failed to create backup archive: %w", err)
	}

	commonBackup.LogArchiveSize(backupFile)
	logger.Infof("✅ Catalog backup completed: %s\n", backupFile)

	return nil
}

// runBackupSteps executes each backup step in sequence.
// A failure in any step aborts the backup cleanly (temp dir is cleaned up by the caller).
func runBackupSteps(ctx context.Context, pc *podman.PodmanClient, baseDir, tempDir string) error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"postgres", func() error { return backupPostgres(ctx, tempDir) }},
		{"secrets", func() error { return backupSecrets(ctx, tempDir) }},
		{"caddy", func() error { return backupCaddyFiles(ctx, baseDir, tempDir) }},
	}

	for _, step := range steps {
		logger.Infof("Backup step: %s\n", step.name)

		if err := step.fn(); err != nil {
			return fmt.Errorf("backup step %q failed: %w", step.name, err)
		}
	}

	return nil
}

// resolveBackupFile returns the provided filename or auto-generates one when empty.
func resolveBackupFile(backupFile string) string {
	if backupFile != "" {
		return backupFile
	}

	timestamp := time.Now().Format("20060102_150405")

	return fmt.Sprintf("catalog_backup_%s.tar.gz", timestamp)
}

// Made with Bob
