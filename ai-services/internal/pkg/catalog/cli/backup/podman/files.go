package podman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// backupCaddyFiles copies all Caddy state from the host BaseDir into tempDir/caddy/:
//
//  1. autosave.json  – Caddy's live config snapshot (mandatory)
//  2. pki/           – Caddy's root CA and intermediate cert/key (always present)
//  3. certificates/  – issued leaf certs per-domain (always present)
//  4. certs/         – user-supplied staged TLS files (only when custom certs are used)
//
// The pki/ directory is backed up in both self-signed and custom-cert scenarios
// because Caddy always maintains its own internal PKI regardless of the TLS mode.
func backupCaddyFiles(ctx context.Context, baseDir, tempDir string) error {
	logger.Infoln("  Backing up Caddy configuration files...")

	caddyDir := filepath.Join(tempDir, catalogpodman.DirCaddy)
	if err := os.MkdirAll(caddyDir, catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create caddy backup directory: %w", err)
	}

	if err := backupAutosave(baseDir, caddyDir); err != nil {
		return err
	}

	if err := backupCaddyPKI(baseDir, caddyDir); err != nil {
		return err
	}

	return backupStagedCerts(baseDir, caddyDir)
}

// backupAutosave copies <BaseDir>/common/caddy-config/caddy/autosave.json.
func backupAutosave(baseDir, caddyDir string) error {
	src := filepath.Join(baseDir, catalogpodman.CaddyConfigSubDir, catalogpodman.AutosaveFileName)

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("caddy autosave.json not found at %s – ensure the catalog is running before taking a backup", src)
	}

	dst := filepath.Join(caddyDir, catalogpodman.AutosaveFileName)
	if err := catalogpodman.CopyFile(src, dst, catalogpodman.FilePerm); err != nil {
		return fmt.Errorf("failed to copy autosave.json: %w", err)
	}

	logger.Infof("  ✓ autosave.json backed up\n")

	return nil
}

// backupCaddyPKI backs up Caddy's internal PKI and leaf-cert store.
// These directories are always present under <BaseDir>/common/caddy/ regardless
// of TLS mode, because Caddy maintains its own root CA even when custom certs
// are loaded via the Admin API.
//
//	pki/authorities/local/   – root CA cert + key (root.crt, root.key, intermediate.crt)
//	certificates/local/      – issued leaf certs per-domain
//
// If a directory does not yet exist (catalog was never fully started) it is
// skipped with a warning rather than failing the entire backup.
func backupCaddyPKI(baseDir, caddyDir string) error {
	caddyDataDir := filepath.Join(baseDir, catalogpodman.CaddyDataSubDir)

	type pkiDir struct {
		subDir string
		label  string
	}

	dirs := []pkiDir{
		{catalogpodman.CaddyPKISubDir, "PKI"},
		{catalogpodman.CaddyLeafCertsSubDir, "leaf certificates"},
	}

	backedUpAny := false

	for _, d := range dirs {
		src := filepath.Join(caddyDataDir, d.subDir)

		if _, err := os.Stat(src); os.IsNotExist(err) {
			logger.Infof("  Caddy %s directory not found at %s – skipping\n", d.label, src)

			continue
		}

		dst := filepath.Join(caddyDir, d.subDir)
		if err := catalogpodman.CopyDir(src, dst); err != nil {
			return fmt.Errorf("failed to back up Caddy %s: %w", d.label, err)
		}

		logger.Infof("  ✓ Backed up Caddy %s\n", d.label)

		backedUpAny = true
	}

	if !backedUpAny {
		logger.Warningln("  Caddy PKI directories not found – Caddy may not have fully started yet")
	}

	return nil
}

// backupStagedCerts copies user-supplied TLS files when custom certificates
// are in use. If no staged files exist (self-signed mode) the step is a no-op.
//
//	<BaseDir>/common/caddy/certs/tls-*.crt
//	<BaseDir>/common/caddy/certs/tls-*.key
func backupStagedCerts(baseDir, caddyDir string) error {
	certsDir := filepath.Join(baseDir, catalogpodman.CaddyDataSubDir, catalogpodman.CaddyCertsSubDir)

	certMatches, _ := filepath.Glob(filepath.Join(certsDir, "tls-*.crt"))
	keyMatches, _ := filepath.Glob(filepath.Join(certsDir, "tls-*.key"))

	if len(certMatches) == 0 && len(keyMatches) == 0 {
		logger.Infoln("  No staged TLS certificates found – self-signed mode active")

		return nil
	}

	destCertsDir := filepath.Join(caddyDir, catalogpodman.CaddyCertsSubDir)
	if err := os.MkdirAll(destCertsDir, catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create certs backup directory: %w", err)
	}

	for _, src := range append(certMatches, keyMatches...) {
		dst := filepath.Join(destCertsDir, filepath.Base(src))
		if err := catalogpodman.CopyFile(src, dst, catalogpodman.FilePerm); err != nil {
			return fmt.Errorf("failed to copy certificate file %s: %w", filepath.Base(src), err)
		}
		logger.Infof("  ✓ Backed up: %s\n", filepath.Base(src))
	}

	logger.Infof("  ✓ Backed up %d user-supplied TLS file(s)\n", len(certMatches)+len(keyMatches))

	return nil
}

// Made with Bob
