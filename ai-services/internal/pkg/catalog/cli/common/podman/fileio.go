// Package podman provides shared utilities for catalog CLI operations
// that need access to the Podman runtime (backup, restore, etc.).
package podman

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Permission bits used consistently for backup and restore operations.
const (
	DirPerm  = 0o700
	FilePerm = 0o600
)

// Archive top-level directory names — identical in backup and restore archives.
const (
	DirDB    = "db"
	DirCaddy = "caddy"
)

// Caddy path constants — relative paths inside BaseDir and inside the archive.
const (
	CaddyDataSubDir      = "common/caddy"
	CaddyConfigSubDir    = "common/caddy-config/caddy"
	AutosaveFileName     = "autosave.json"
	CaddyCertsSubDir     = "certs"
	CaddyPKISubDir       = "pki"
	CaddyLeafCertsSubDir = "certificates"
)

// Caddy pod name — constructed from CatalogAppName + "--caddy".
const (
	CaddyPodName = "ai-services--caddy"
)

// Postgres constants shared between backup and restore.
const (
	DBDumpFileName    = "catalog-db.sql"
	PostgresContainer = "postgresql"
	PostgresUser      = "admin"
	PostgresDB        = "ai_services"
	StderrBufferBytes = 4096
)

// CopyFile copies src to dst with the given permission bits.
// The destination file is created (or truncated) atomically via O_TRUNC.
func CopyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	return nil
}

// CopyDir recursively copies the directory tree rooted at src into dst,
// preserving the relative structure.  dst is created if it does not exist.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, DirPerm)
		}

		return CopyFile(path, target, FilePerm)
	})
}

// Made with Bob
