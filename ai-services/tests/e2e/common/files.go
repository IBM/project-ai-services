package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const dirPerm = 0o755 // standard permission for directories.

// EnsureDir creates a directory if it does not exist.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, dirPerm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}

	logger.Infof("Directory ensured:", path)

	return nil
}

// CopyDirFiltered copies files from src to dst based on filter.
func CopyDirFiltered(src, dst string, allow func(name string) bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source dir %s: %w", src, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !allow(entry.Name()) {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := in.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	if err = out.Sync(); err != nil {
		return err
	}

	return nil
}
