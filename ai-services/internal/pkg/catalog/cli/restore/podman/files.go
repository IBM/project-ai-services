package podman

import (
	"fmt"
	"os"
	"path/filepath"

	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// restoreCaddyFiles restores all Caddy state from the backup archive to the host BaseDir:
//
//  1. autosave.json  – Caddy's live config snapshot (mandatory)
//  2. pki/           – Caddy's root CA and intermediate cert/key (if present in backup)
//  3. certificates/  – issued leaf certs per-domain (if present in backup)
//  4. certs/         – user-supplied staged TLS files (if present in backup)
//
// Existing files are overwritten in-place so that Caddy picks them up on the
// next startup without further intervention.
func restoreCaddyFiles(baseDir, tempDir string) error {
	logger.Infoln("  Restoring Caddy configuration files...")

	caddyBackupDir := filepath.Join(tempDir, catalogpodman.DirCaddy)

	if err := restoreAutosave(baseDir, caddyBackupDir); err != nil {
		return err
	}

	if err := restoreCaddyPKI(baseDir, caddyBackupDir); err != nil {
		return err
	}

	return restoreStagedCerts(baseDir, caddyBackupDir)
}

// restoreAutosave copies autosave.json from the backup to BaseDir.
func restoreAutosave(baseDir, caddyBackupDir string) error {
	src := filepath.Join(caddyBackupDir, catalogpodman.AutosaveFileName)

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("autosave.json not found in backup")
	}

	dst := filepath.Join(baseDir, catalogpodman.CaddyConfigSubDir, catalogpodman.AutosaveFileName)

	// Ensure the destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create caddy-config directory: %w", err)
	}

	if err := catalogpodman.CopyFile(src, dst, catalogpodman.FilePerm); err != nil {
		return fmt.Errorf("failed to restore autosave.json: %w", err)
	}

	logger.Infoln("  ✓ autosave.json restored")

	return nil
}

// restoreCaddyPKI restores Caddy's internal PKI (pki/) and leaf-cert store (certificates/).
// If a directory is absent in the backup it is silently skipped.
func restoreCaddyPKI(baseDir, caddyBackupDir string) error {
	caddyDataDir := filepath.Join(baseDir, catalogpodman.CaddyDataSubDir)

	type pkiDir struct {
		subDir string
		label  string
	}

	dirs := []pkiDir{
		{catalogpodman.CaddyPKISubDir, "PKI"},
		{catalogpodman.CaddyLeafCertsSubDir, "leaf certificates"},
	}

	for _, d := range dirs {
		src := filepath.Join(caddyBackupDir, d.subDir)

		if _, err := os.Stat(src); os.IsNotExist(err) {
			logger.Infof("  Caddy %s not found in backup – skipping\n", d.label)

			continue
		}

		dst := filepath.Join(caddyDataDir, d.subDir)
		if err := os.MkdirAll(dst, catalogpodman.DirPerm); err != nil {
			return fmt.Errorf("failed to create Caddy %s directory: %w", d.label, err)
		}

		if err := catalogpodman.CopyDir(src, dst); err != nil {
			return fmt.Errorf("failed to restore Caddy %s: %w", d.label, err)
		}

		logger.Infof("  ✓ Restored Caddy %s\n", d.label)
	}

	return nil
}

// restoreStagedCerts restores user-supplied TLS certificates from the backup.
// If no staged certificates exist in the backup (self-signed mode), the step is a no-op.
func restoreStagedCerts(baseDir, caddyBackupDir string) error {
	srcCertsDir := filepath.Join(caddyBackupDir, catalogpodman.CaddyCertsSubDir)

	// If the certs sub-directory was not included in the backup, skip silently.
	if _, err := os.Stat(srcCertsDir); os.IsNotExist(err) {
		logger.Infoln("  No staged TLS certificates in backup – self-signed mode, skipping")

		return nil
	}

	certMatches, _ := filepath.Glob(filepath.Join(srcCertsDir, "tls-*.crt"))
	keyMatches, _ := filepath.Glob(filepath.Join(srcCertsDir, "tls-*.key"))

	if len(certMatches) == 0 && len(keyMatches) == 0 {
		logger.Infoln("  No staged TLS certificate files found in backup – skipping")

		return nil
	}

	dstCertsDir := filepath.Join(baseDir, catalogpodman.CaddyDataSubDir, catalogpodman.CaddyCertsSubDir)
	if err := os.MkdirAll(dstCertsDir, catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create certs destination directory: %w", err)
	}

	for _, src := range append(certMatches, keyMatches...) {
		dst := filepath.Join(dstCertsDir, filepath.Base(src))
		if err := catalogpodman.CopyFile(src, dst, catalogpodman.FilePerm); err != nil {
			return fmt.Errorf("failed to restore certificate file %s: %w", filepath.Base(src), err)
		}
		logger.Infof("  ✓ Restored: %s\n", filepath.Base(src))
	}

	logger.Infof("  ✓ Restored %d user-supplied TLS file(s)\n", len(certMatches)+len(keyMatches))

	return nil
}

// Made with Bob
