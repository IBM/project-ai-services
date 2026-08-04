package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	defaultFilePermission = 0o644
	defaultDirPermission  = 0o755
	bytesPerKB            = 1024
	bytesPerMB            = bytesPerKB * 1024
)

// CreateTarGzArchive creates a tar.gz archive from a source directory.
// It includes the specified entries (files or directories) in the archive.
func CreateTarGzArchive(sourceDir, targetFile string, entries []string) error {
	file, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("failed to create backup archive: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	gzipWriter := gzip.NewWriter(file)
	defer func() {
		_ = gzipWriter.Close()
	}()

	tarWriter := tar.NewWriter(gzipWriter)
	defer func() {
		_ = tarWriter.Close()
	}()

	for _, entry := range entries {
		fullPath := filepath.Join(sourceDir, entry)
		if err := addPathToTar(tarWriter, sourceDir, fullPath); err != nil {
			return err
		}
	}

	return nil
}

// addPathToTar adds a file or directory to the tar archive.
func addPathToTar(tarWriter *tar.Writer, baseDir, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat path %s: %w", path, err)
	}

	relPath, err := filepath.Rel(baseDir, path)
	if err != nil {
		return fmt.Errorf("failed to determine relative path for %s: %w", path, err)
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create tar header for %s: %w", path, err)
	}
	header.Name = filepath.ToSlash(relPath)

	if info.IsDir() && header.Name[len(header.Name)-1] != '/' {
		header.Name += "/"
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", path, err)
	}

	if info.IsDir() {
		return addDirectoryToTar(tarWriter, baseDir, path)
	}

	return addFileToTar(tarWriter, path)
}

// addDirectoryToTar recursively adds a directory's contents to the tar archive.
func addDirectoryToTar(tarWriter *tar.Writer, baseDir, path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	for _, entry := range entries {
		if err := addPathToTar(tarWriter, baseDir, filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

// addFileToTar adds a file's contents to the tar archive.
func addFileToTar(tarWriter *tar.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := io.Copy(tarWriter, file); err != nil {
		return fmt.Errorf("failed to write file contents for %s: %w", path, err)
	}

	return nil
}

// LogArchiveSize logs the size of the created archive file.
func LogArchiveSize(backupFile string) {
	fileInfo, err := os.Stat(backupFile)
	if err == nil {
		sizeMB := float64(fileInfo.Size()) / bytesPerMB
		logger.Infof("✓ Tar archive created: %s (%.2f MB)\n", backupFile, sizeMB)

		return
	}

	logger.Infof("✓ Tar archive created: %s\n", backupFile)
}

// Made with Bob
