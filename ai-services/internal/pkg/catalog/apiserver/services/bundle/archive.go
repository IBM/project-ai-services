package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

const (
	// BundleFileExtension is the required file extension for bundle uploads.
	BundleFileExtension = ".tar.gz"

	// bundleStorageRoot is the mount path for the dedicated catalog-bundles volume.
	bundleStorageRoot = "/data/catalog-bundles"

	// maxExtractedFileSize is the aggregate uncompressed size limit enforced during
	// extraction (50 MB). If the total bytes written across all files exceeds this
	// value the extraction is aborted.
	maxExtractedFileSize int64 = 50 * 1024 * 1024

	// splitTwo is the n argument for strings.SplitN when splitting on the first slash.
	splitTwo = 2

	// dirPerm is the permission bits used when creating directories during extraction.
	// 0o750: owner=rwx, group=rx, other=none. The bundle volume is a container-private
	// mount; world access is blocked while group read/execute is retained for any
	// supplementary GID the container runtime assigns to the apiserver process.
	dirPerm = 0o750
)

// bundleDirPath returns the canonical on-disk directory for a bundle.
//
// Layout: <bundleStorageRoot>/<type-plural>/<catalog_id>-<version>
//
// Examples:
//
//	service   my-service      1.0.0  → /data/catalog-bundles/services/my-service-1.0.0
//	component llm--my-prov    1.0.0  → /data/catalog-bundles/components/llm--my-prov-1.0.0
func bundleDirPath(catalogType, catalogID, version string) string {
	subdir := catalogTypeToDir(catalogType)

	return filepath.Join(bundleStorageRoot, subdir, catalogID+"-"+version)
}

// catalogTypeToDir returns the plural on-disk subdirectory name for a catalog type.
func catalogTypeToDir(catalogType string) string {
	return catalogType + "s"
}

// peekMetadata reads the entire archive into memory, locates the root metadata.yaml,
// parses all required fields, and returns:
//   - the raw archive bytes (so callers can hand the same bytes to extractAndMeasure
//     without re-reading the original io.Reader),
//   - either a *bundlemetadata.ServiceMetadata or *bundlemetadata.ComponentMetadata, and
//   - the top-level directory name inferred from the archive structure (empty for flat archives).
//
// Threading topDir out avoids a redundant extra scan by each runtime validator, which
// would otherwise call resolveTopDir / inferTopDir on the same bytes a second time.
//
// Returns *ValidationError{Code:400} on I/O or archive errors and
// *ValidationError{Code:422} on missing/invalid metadata fields.
func peekMetadata(r io.Reader) ([]byte, any, string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, "", &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to read archive: %s", err),
		}
	}

	meta, topDir, err := parseMetadataFromBytes(data)
	if err != nil {
		return nil, nil, "", err
	}

	return data, meta, topDir, nil
}

// parseMetadataFromBytes walks the gzip-compressed tar archive stored in data,
// finds the root metadata.yaml (either at the top level or one directory deep),
// and delegates to parseMetadataYAML.
// It also returns the inferred topDir so callers do not need a second scan.
func parseMetadataFromBytes(data []byte) (any, string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, "", &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid gzip archive: %s", err),
		}
	}
	defer func() { _ = gr.Close() }()

	return scanArchiveForMetadata(tar.NewReader(gr))
}

// scanArchiveForMetadata walks tar entries looking for the root metadata.yaml
// and delegates to parseMetadataYAML once found.
// topDir is inferred from the first entry that contains a slash; flat archives
// (no top-level directory) are handled via the bare "metadata.yaml" match.
// It returns both the parsed metadata and the inferred topDir.
func scanArchiveForMetadata(tr *tar.Reader) (any, string, error) {
	var topDir string

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("error reading archive: %s", err),
			}
		}

		if topDir == "" && strings.Contains(hdr.Name, "/") {
			topDir = strings.SplitN(hdr.Name, "/", splitTwo)[0]
		}

		meta, done, err := tryReadMetadataEntry(tr, hdr, topDir)
		if err != nil {
			return nil, "", err
		}
		if done {
			return meta, topDir, nil
		}
	}

	return nil, "", &validators.ValidationError{
		Code:    http.StatusBadRequest,
		Message: "metadata.yaml not found in archive root",
	}
}

// tryReadMetadataEntry checks whether hdr is the root metadata.yaml entry and,
// if so, reads and parses it. Returns (meta, true, nil) on success,
// (nil, false, nil) when hdr is not the target entry, and (nil, false, err) on error.
func tryReadMetadataEntry(tr *tar.Reader, hdr *tar.Header, topDir string) (any, bool, error) {
	name := filepath.ToSlash(hdr.Name)
	isRootMeta := name == "metadata.yaml" ||
		(topDir != "" && name == topDir+"/metadata.yaml")

	if !isRootMeta || hdr.Typeflag == tar.TypeDir {
		return nil, false, nil
	}

	yamlBytes, err := io.ReadAll(tr)
	if err != nil {
		return nil, false, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to read metadata.yaml: %s", err),
		}
	}

	meta, err := parseMetadataYAML(yamlBytes)
	if err != nil {
		return nil, false, err
	}

	return meta, true, nil
}

// parseMetadataYAML decodes raw YAML bytes, validates all required fields via
// bundlemetadata.ParseRootMetadata, and returns the appropriate concrete type.
// Returns *bundlemetadata.ServiceMetadata or *bundlemetadata.ComponentMetadata.
func parseMetadataYAML(data []byte) (any, error) {
	parsed, err := bundlemetadata.ParseRootMetadata(data)
	if err != nil {
		return nil, err
	}

	switch m := parsed.(type) {
	case *bundlemetadata.ServiceMetadataYAML:
		return &bundlemetadata.ServiceMetadata{
			ID:          m.ID,
			Type:        m.Type,
			Ver:         m.Version,
			DisplayName: m.Name,
		}, nil
	case *bundlemetadata.ComponentMetadataYAML:
		return &bundlemetadata.ComponentMetadata{
			ID:            m.ID,
			Type:          m.Type,
			ComponentType: m.ComponentType,
			Ver:           m.Version,
			DisplayName:   m.Name,
		}, nil
	default:
		// ParseRootMetadata already rejects unknown types before we reach here,
		// so this branch is only reachable if a new type is added without a matching case.
		return nil, fmt.Errorf("parseMetadataYAML: unhandled metadata type %T — add a case branch", parsed)
	}
}

// extractAndMeasure extracts data (a gzip-compressed tar archive) into destDir,
// stripping the single top-level directory prefix that most tools add when
// creating archives. Returns the total uncompressed size in bytes.
//
// Safety guarantees:
//   - Path-traversal guard: any entry whose resolved path escapes destDir is rejected.
//   - Per-file size guard: regular files larger than maxExtractedFileSize are rejected.
//
// Only regular files and directories are extracted; symlinks and other special
// entry types are silently skipped.
func extractAndMeasure(data []byte, destDir string) (int64, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid gzip archive: %s", err),
		}
	}
	defer func() { _ = gr.Close() }()

	return extractEntries(tar.NewReader(gr), destDir)
}

// extractEntries iterates over all tar entries and writes regular files and
// directories to destDir, returning the total uncompressed bytes written.
func extractEntries(tr *tar.Reader, destDir string) (int64, error) {
	var topDir string
	var totalSize int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("error reading archive entry: %w", err)
		}

		n, err := processEntry(tr, hdr, destDir, &topDir)
		if err != nil {
			return 0, err
		}
		totalSize += n
		if totalSize > maxExtractedFileSize {
			return 0, &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: "archive exceeds the 50 MB uncompressed size limit",
			}
		}
	}

	return totalSize, nil
}

// processEntry handles a single tar entry: it strips the top-level directory
// prefix, enforces path-traversal and size guards, and writes the entry to disk.
// Returns the number of bytes written (0 for directories and skipped entries).
func processEntry(tr *tar.Reader, hdr *tar.Header, destDir string, topDir *string) (int64, error) {
	// Strip the top-level directory prefix (e.g. "my-bundle/") on the first
	// entry that contains a slash, then apply it to all subsequent entries.
	name := filepath.ToSlash(hdr.Name)
	if *topDir == "" && strings.Contains(name, "/") {
		*topDir = strings.SplitN(name, "/", splitTwo)[0] + "/"
	}

	relName := strings.TrimPrefix(name, *topDir)
	if relName == "" || relName == "." {
		return 0, nil
	}

	destPath, err := resolveDestPath(destDir, relName, hdr.Name)
	if err != nil {
		return 0, err
	}

	return writeEntry(tr, hdr, destPath)
}

// resolveDestPath joins destDir and relName, then checks that the result stays
// inside destDir (path-traversal guard). Returns the resolved path or an error.
func resolveDestPath(destDir, relName, entryName string) (string, error) {
	destPath := filepath.Join(destDir, relName)
	if !strings.HasPrefix(
		filepath.Clean(destPath)+string(os.PathSeparator),
		filepath.Clean(destDir)+string(os.PathSeparator),
	) {
		return "", &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("path traversal detected in archive entry %q", entryName),
		}
	}

	return destPath, nil
}

// writeEntry writes a single tar entry (directory or regular file) to destPath.
// Symlinks and other special types are silently skipped.
// Returns the number of bytes written (0 for directories and skipped entries).
func writeEntry(tr *tar.Reader, hdr *tar.Header, destPath string) (int64, error) {
	switch hdr.Typeflag {
	case tar.TypeDir:
		if mkErr := os.MkdirAll(destPath, dirPerm); mkErr != nil {
			return 0, fmt.Errorf("failed to create directory %q: %w", destPath, mkErr)
		}

	case tar.TypeReg:
		return writeRegularFile(tr, hdr, destPath)
	}
	// Symlinks and other special types are intentionally skipped.

	return 0, nil
}

// writeRegularFile writes a regular tar file entry to destPath.
func writeRegularFile(tr *tar.Reader, hdr *tar.Header, destPath string) (int64, error) {
	if mkErr := os.MkdirAll(filepath.Dir(destPath), dirPerm); mkErr != nil {
		return 0, fmt.Errorf("failed to create parent directory for %q: %w", destPath, mkErr)
	}

	return writeFile(destPath, tr, hdr.Mode)
}

// writeFile creates (or truncates) the file at path and streams content from r.
// Returns the number of bytes written.
func writeFile(path string, r io.Reader, mode int64) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
	if err != nil {
		return 0, fmt.Errorf("failed to create file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, r)
	if err != nil {
		return 0, fmt.Errorf("failed to write file %q: %w", path, err)
	}

	return n, nil
}
