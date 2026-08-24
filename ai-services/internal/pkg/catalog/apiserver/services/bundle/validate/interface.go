// Package validate defines the BundleValidator interface and the per-runtime
// implementations that inspect a bundle archive before it is persisted.
//
// Bundle validation is intentionally split by runtime because the required
// directory layout and file constraints differ:
//
//   - Podman bundles must contain a `podman/` sub-directory with a `metadata.yaml`,
//     `values.yaml`, `values.schema.json`, and at least one `templates/*.yaml.tmpl`.
//
//   - OpenShift bundles must contain an `openshift/` sub-directory with a
//     `Chart.yaml`, `values.yaml`, `values.schema.json`, and at least one
//     `templates/*.yaml`.
//
// The shared caller (bundleService.ValidateBundle) reads the root metadata.yaml
// to determine the catalog type (service / component) and then calls each
// registered validator in turn. All validators receive the same raw archive
// bytes so they can walk the tar stream independently without re-reading the
// original io.Reader.
package validate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// BundleValidator is implemented by each runtime-specific validator.
// Validate receives the full archive bytes (already read into memory by
// peekMetadata), the top-level service/component directory name as inferred
// from the archive structure, and the version declared in the root metadata.yaml
// so each runtime can enforce that all version fields are consistent.
//
// A validator must return a *validators.ValidationError on failure so the
// caller can map it to the correct HTTP status (400 or 422).
// A nil error means the archive is valid for this runtime.
type BundleValidator interface {
	// Validate inspects the archive for runtime-specific structural requirements.
	// archiveBytes is the raw .tar.gz content; topDir is the inferred top-level
	// directory name inside the archive (may be empty for flat archives);
	// rootVersion is the version from the root metadata.yaml that all runtime
	// metadata versions must match.
	Validate(archiveBytes []byte, topDir, rootVersion string) error
}

// -----------------------------------------------------------------------
// Shared archive helpers
// -----------------------------------------------------------------------

// scanEntries walks a gzip-compressed tar archive and calls visit for every
// entry. visit receives the normalised forward-slash path (header name) and
// the tar.Header. The visit function should return true to stop iteration.
func scanEntries(archiveBytes []byte, visit func(name string, hdr *tar.Header) (stop bool, err error)) error {
	gr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid gzip archive: %s", err),
		}
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("error reading archive: %s", err),
			}
		}

		name := filepath.ToSlash(hdr.Name)
		stop, visitErr := visit(name, hdr)
		if visitErr != nil {
			return visitErr
		}
		if stop {
			break
		}
	}

	return nil
}

// scanEntriesWithContent is like scanEntries but also reads each entry's content
// and passes it to visit. It is used when callers need the raw bytes of each
// file (e.g. to build []*archive.BufferedFile for in-memory Helm chart loading).
// Directory entries are also visited, but content will be nil for them.
func scanEntriesWithContent(archiveBytes []byte, visit func(name string, hdr *tar.Header, content []byte) (stop bool, err error)) error {
	gr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid gzip archive: %s", err),
		}
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("error reading archive: %s", err),
			}
		}

		var content []byte
		if hdr.Typeflag == tar.TypeReg {
			content, err = io.ReadAll(tr)
			if err != nil {
				return &validators.ValidationError{
					Code:    http.StatusBadRequest,
					Message: fmt.Sprintf("error reading archive entry %q: %s", hdr.Name, err),
				}
			}
		}

		name := filepath.ToSlash(hdr.Name)
		stop, visitErr := visit(name, hdr, content)
		if visitErr != nil {
			return visitErr
		}
		if stop {
			break
		}
	}

	return nil
}

// stripTopDir removes the optional single top-level directory prefix from path.
// e.g. stripTopDir("my-service/podman/values.yaml", "my-service") → "podman/values.yaml"
// When topDir is empty or the path does not start with it, path is returned as-is.
func stripTopDir(path, topDir string) string {
	if topDir == "" {
		return path
	}
	prefix := topDir + "/"

	return strings.TrimPrefix(path, prefix)
}

// InferTopDir returns the top-level directory name from the first archive entry
// that contains a slash, or "" for flat archives.
// It is exported so callers in the parent bundle package can reuse the same
// inference without duplicating archive-walking logic.
func InferTopDir(archiveBytes []byte) (string, error) {
	var topDir string
	err := scanEntries(archiveBytes, func(name string, _ *tar.Header) (bool, error) {
		if strings.Contains(name, "/") {
			topDir = strings.SplitN(name, "/", 2)[0] //nolint:mnd

			return true, nil
		}

		return false, nil
	})

	return topDir, err
}

// resolveTopDir returns topDir unchanged when it is already known, or infers it
// from the archive when it is empty. Used by both runtime validators.
func resolveTopDir(archiveBytes []byte, topDir string) (string, error) {
	if topDir != "" {
		return topDir, nil
	}

	return InferTopDir(archiveBytes)
}
