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

	logger.Infof("Directory ensured: %v", path)

	return nil
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string) (err error) {
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

// RemoveDirContents deletes the contents of a directory.
func RemoveDirContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		if derr := d.Close(); derr != nil && err == nil {
			err = derr
		}
	}()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		itemPath := filepath.Join(dir, name)
		err = os.RemoveAll(itemPath)
		if err != nil {
			return err
		}
	}

	return nil
}
