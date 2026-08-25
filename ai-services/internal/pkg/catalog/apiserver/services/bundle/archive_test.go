package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Shared archive builder (used by this file and service_test.go)
// -----------------------------------------------------------------------

// buildArchive creates a gzip-compressed tar archive in memory.
// entries maps relative path → file content. If wrapInTopDir is true,
// all entries are placed under a "bundle/" top-level directory, which is
// the layout most archive tools produce.
func buildArchive(t *testing.T, entries map[string]string, wrapInTopDir bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	prefix := ""
	if wrapInTopDir {
		prefix = "bundle/"
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     "bundle/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		}))
	}

	for name, content := range entries {
		body := []byte(content)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     prefix + name,
			Typeflag: tar.TypeReg,
			Size:     int64(len(body)),
			Mode:     0o644,
		}))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// serviceMetaYAML returns a minimal valid service metadata.yaml body.
// name may be empty; a placeholder is used when omitted so the 'name' check passes.
func serviceMetaYAML(id, version, name string) string {
	n := name
	if n == "" {
		n = id
	}
	return "id: " + id + "\ntype: service\nversion: " + version + "\nname: " + n + "\ndescription: test\nstandalone: true\n"
}

// componentMetaYAML returns a minimal valid component metadata.yaml body.
func componentMetaYAML(id, componentType, version string) string {
	return "id: " + id + "\ntype: component\ncomponent_type: " + componentType + "\nversion: " + version + "\nname: " + id + "\ndescription: test\n"
}

// -----------------------------------------------------------------------
// bundleDirPath / catalogTypeToDir
// -----------------------------------------------------------------------

func TestBundleDirPath_Service(t *testing.T) {
	got := bundleDirPath(bundlemetadata.CatalogTypeService, "my-service", "1.0.0")
	assert.Equal(t, "/data/catalog-bundles/services/my-service-1.0.0", got)
}

func TestBundleDirPath_Component(t *testing.T) {
	got := bundleDirPath(bundlemetadata.CatalogTypeComponent, "llm--my-provider", "2.1.0")
	assert.Equal(t, "/data/catalog-bundles/components/llm--my-provider-2.1.0", got)
}

func TestBundleDirPath_UnknownTypeUsesPlural(t *testing.T) {
	// Unknown catalog types fall back to <type>+"s" so future types degrade gracefully.
	got := bundleDirPath("widget", "my-widget", "1.0.0")
	assert.Equal(t, "/data/catalog-bundles/widgets/my-widget-1.0.0", got)
}

func TestBundleDirPath_TypePluralisation(t *testing.T) {
	// bundleDirPath appends "s" to the catalog type for the on-disk subdirectory.
	tests := []struct{ catalogType, want string }{
		{bundlemetadata.CatalogTypeService, "/data/catalog-bundles/services/svc-1.0.0"},
		{bundlemetadata.CatalogTypeComponent, "/data/catalog-bundles/components/svc-1.0.0"},
		{"architecture", "/data/catalog-bundles/architectures/svc-1.0.0"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, bundleDirPath(tt.catalogType, "svc", "1.0.0"), "catalogType=%s", tt.catalogType)
	}
}

// -----------------------------------------------------------------------
// peekMetadata
// -----------------------------------------------------------------------

func TestPeekMetadata_ServiceWrapped(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": serviceMetaYAML("my-service", "1.0.0", "My Service"),
	}, true)

	data, meta, _, err := peekMetadata(bytes.NewReader(archive))
	require.NoError(t, err)
	// raw bytes returned must equal the original archive
	assert.Equal(t, archive, data)
	sm := meta.(*bundlemetadata.ServiceMetadata)
	assert.Equal(t, "my-service", sm.ID)
	assert.Equal(t, bundlemetadata.CatalogTypeService, sm.Type)
	assert.Equal(t, "1.0.0", sm.Ver)
	assert.Equal(t, "My Service", sm.DisplayName)
}

func TestPeekMetadata_ServiceFlat(t *testing.T) {
	// Flat archive — no top-level directory, metadata.yaml is at root.
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": serviceMetaYAML("flat-svc", "2.0.0", "Flat"),
	}, false)

	_, meta, _, err := peekMetadata(bytes.NewReader(archive))
	require.NoError(t, err)
	sm := meta.(*bundlemetadata.ServiceMetadata)
	assert.Equal(t, "flat-svc", sm.ID)
	assert.Equal(t, bundlemetadata.CatalogTypeService, sm.Type)
	assert.Equal(t, "2.0.0", sm.Ver)
}

func TestPeekMetadata_ComponentWrapped(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": componentMetaYAML("my-provider", "llm", "1.0.0"),
	}, true)

	_, meta, _, err := peekMetadata(bytes.NewReader(archive))
	require.NoError(t, err)
	cm := meta.(*bundlemetadata.ComponentMetadata)
	// CatalogID is the composite <component_type>--<id>
	assert.Equal(t, "llm--my-provider", cm.ComponentType+"--"+cm.ID)
	assert.Equal(t, bundlemetadata.CatalogTypeComponent, cm.Type)
	assert.Equal(t, "1.0.0", cm.Ver)
	assert.Equal(t, "llm", cm.ComponentType)
}

func TestPeekMetadata_MetadataNestedDeeper_NotFound(t *testing.T) {
	// metadata.yaml buried two levels deep should NOT be found
	archive := buildArchive(t, map[string]string{
		"subdir/metadata.yaml": serviceMetaYAML("deep", "1.0.0", ""),
	}, true)

	_, _, _, err := peekMetadata(bytes.NewReader(archive))
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadRequest, valErr.Code)
	assert.Contains(t, valErr.Message, "metadata.yaml not found")
}

func TestPeekMetadata_NotGzip(t *testing.T) {
	_, _, _, err := peekMetadata(bytes.NewReader([]byte("not a gzip")))
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadRequest, valErr.Code)
	assert.Contains(t, valErr.Message, "invalid gzip")
}

func TestPeekMetadata_EmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	require.NoError(t, gzip.NewWriter(&buf).Close()) // minimal valid gzip, no tar content
	require.NoError(t, gw.Close())

	_, _, _, err := peekMetadata(bytes.NewReader(buf.Bytes()))
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadRequest, valErr.Code)
}

func TestPeekMetadata_NoMetadataYAML(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"other.yaml": "irrelevant: true\n",
	}, true)

	_, _, _, err := peekMetadata(bytes.NewReader(archive))
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadRequest, valErr.Code)
	assert.Contains(t, valErr.Message, "metadata.yaml not found")
}

func TestPeekMetadata_ReturnedBytesCanBeReused(t *testing.T) {
	// Verify that the returned bytes slice is an independent copy that can be
	// passed to extractAndMeasure (or any second reader) without error.
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": serviceMetaYAML("svc", "1.0.0", ""),
	}, true)

	data, _, _, err := peekMetadata(bytes.NewReader(archive))
	require.NoError(t, err)

	// A second peek on the returned bytes must succeed.
	_, meta2, _, err := peekMetadata(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "svc", meta2.(*bundlemetadata.ServiceMetadata).ID)
}

// -----------------------------------------------------------------------
// parseMetadataYAML
// -----------------------------------------------------------------------

func TestParseMetadataYAML_ServiceMinimal(t *testing.T) {
	meta, err := parseMetadataYAML([]byte("id: svc\ntype: service\nversion: 1.0.0\nname: n\ndescription: d\nstandalone: true\n"))
	require.NoError(t, err)
	sm := meta.(*bundlemetadata.ServiceMetadata)
	assert.Equal(t, "svc", sm.ID)
	assert.Equal(t, bundlemetadata.CatalogTypeService, sm.Type)
	assert.Equal(t, "1.0.0", sm.Ver)
}

func TestParseMetadataYAML_ServiceWithName(t *testing.T) {
	meta, err := parseMetadataYAML([]byte("id: svc\ntype: service\nversion: 1.2.3\nname: My Svc\ndescription: d\nstandalone: true\n"))
	require.NoError(t, err)
	assert.Equal(t, "My Svc", meta.(*bundlemetadata.ServiceMetadata).DisplayName)
}

func TestParseMetadataYAML_ComponentAllFields(t *testing.T) {
	meta, err := parseMetadataYAML([]byte(
		"id: prov\ntype: component\ncomponent_type: embedding\nversion: 3.0.0\nname: My Embedder\ndescription: d\n",
	))
	require.NoError(t, err)
	cm := meta.(*bundlemetadata.ComponentMetadata)
	assert.Equal(t, "embedding--prov", cm.ComponentType+"--"+cm.ID)
	assert.Equal(t, bundlemetadata.CatalogTypeComponent, cm.Type)
	assert.Equal(t, "3.0.0", cm.Ver)
	assert.Equal(t, "My Embedder", cm.DisplayName)
	assert.Equal(t, "embedding", cm.ComponentType)
}

func TestParseMetadataYAML_MissingID(t *testing.T) {
	_, err := parseMetadataYAML([]byte("type: service\nversion: 1.0.0\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'id' is required")
}

func TestParseMetadataYAML_MissingType(t *testing.T) {
	_, err := parseMetadataYAML([]byte("id: svc\nversion: 1.0.0\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'type' is required")
}

func TestParseMetadataYAML_MissingVersion(t *testing.T) {
	_, err := parseMetadataYAML([]byte("id: svc\ntype: service\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

func TestParseMetadataYAML_ComponentMissingComponentType(t *testing.T) {
	_, err := parseMetadataYAML([]byte("id: prov\ntype: component\nversion: 1.0.0\nname: n\ndescription: d\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'component_type' is required")
}

func TestParseMetadataYAML_UnknownType(t *testing.T) {
	_, err := parseMetadataYAML([]byte("id: x\ntype: architecture\nversion: 1.0.0\nname: n\ndescription: d\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "unsupported type")
}

func TestParseMetadataYAML_InvalidYAML(t *testing.T) {
	_, err := parseMetadataYAML([]byte(":\tinvalid yaml"))
	assertValidationError(t, err, http.StatusBadRequest, "")
}

// -----------------------------------------------------------------------
// extractAndMeasure
// -----------------------------------------------------------------------

func TestExtractAndMeasure_ValidWrappedArchive(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":           serviceMetaYAML("svc", "1.0.0", ""),
		"podman/values.yaml":      "key: value\n",
		"podman/templates/a.tmpl": "template content\n",
	}, true)

	destDir := t.TempDir()
	size, err := extractAndMeasure(archive, destDir)
	require.NoError(t, err)
	assert.Positive(t, size)

	// Verify files were written to the right locations.
	assertFileExists(t, filepath.Join(destDir, "metadata.yaml"))
	assertFileExists(t, filepath.Join(destDir, "podman", "values.yaml"))
	assertFileExists(t, filepath.Join(destDir, "podman", "templates", "a.tmpl"))
}

func TestExtractAndMeasure_ValidFlatArchive(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": serviceMetaYAML("svc", "1.0.0", ""),
		"values.yaml":   "key: value\n",
	}, false)

	destDir := t.TempDir()
	size, err := extractAndMeasure(archive, destDir)
	require.NoError(t, err)
	assert.Positive(t, size)
	assertFileExists(t, filepath.Join(destDir, "metadata.yaml"))
	assertFileExists(t, filepath.Join(destDir, "values.yaml"))
}

func TestExtractAndMeasure_SizeAccumulates(t *testing.T) {
	content := "hello world\n" // 12 bytes
	archive := buildArchive(t, map[string]string{
		"a.txt": content,
		"b.txt": content,
	}, true)

	destDir := t.TempDir()
	size, err := extractAndMeasure(archive, destDir)
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)*2), size)
}

func TestExtractAndMeasure_DirectoryEntriesCreated(t *testing.T) {
	// Build archive with an explicit directory entry plus a file inside it.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}))
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/subdir/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}))
	body := []byte("content")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/subdir/file.txt",
		Typeflag: tar.TypeReg,
		Size:     int64(len(body)),
		Mode:     0o644,
	}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	destDir := t.TempDir()
	_, err = extractAndMeasure(buf.Bytes(), destDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(destDir, "subdir"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assertFileExists(t, filepath.Join(destDir, "subdir", "file.txt"))
}

func TestExtractAndMeasure_SymlinksSkipped(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add a symlink entry — should be silently skipped.
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/link",
		Typeflag: tar.TypeSymlink,
		Linkname: "/etc/passwd",
	}))
	// Add a regular file so size > 0 confirms the archive was processed.
	body := []byte("real content")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/real.txt",
		Typeflag: tar.TypeReg,
		Size:     int64(len(body)),
		Mode:     0o644,
	}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	destDir := t.TempDir()
	size, err := extractAndMeasure(buf.Bytes(), destDir)
	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), size)

	// Symlink must not have been created.
	_, statErr := os.Lstat(filepath.Join(destDir, "link"))
	assert.True(t, os.IsNotExist(statErr), "symlink should not be created")
}

func TestExtractAndMeasure_PathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := []byte("evil")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/../../../etc/passwd",
		Typeflag: tar.TypeReg,
		Size:     int64(len(body)),
		Mode:     0o644,
	}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	destDir := t.TempDir()
	_, err = extractAndMeasure(buf.Bytes(), destDir)
	assertValidationError(t, err, http.StatusBadRequest, "path traversal")
}

func TestExtractAndMeasure_AggregateLimitExceeded_SingleFile(t *testing.T) {
	// A single file whose size exceeds the aggregate limit must be rejected.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	oversizedLen := maxExtractedFileSize + 1
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "bundle/big.bin",
		Typeflag: tar.TypeReg,
		Size:     oversizedLen,
		Mode:     0o644,
	}))
	_, err := io.Copy(tw, io.LimitReader(zeroReader{}, oversizedLen))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	_, err = extractAndMeasure(buf.Bytes(), t.TempDir())
	assertValidationError(t, err, http.StatusBadRequest, "50 MB")
}

func TestExtractAndMeasure_AggregateLimitExceeded_MultipleSmallFiles(t *testing.T) {
	// Two files each just above half the limit — individually fine, collectively over.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	halfPlus := maxExtractedFileSize/2 + 1
	for _, name := range []string{"bundle/a.bin", "bundle/b.bin"} {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Size:     halfPlus,
			Mode:     0o644,
		}))
		_, err := io.Copy(tw, io.LimitReader(zeroReader{}, halfPlus))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	_, err := extractAndMeasure(buf.Bytes(), t.TempDir())
	assertValidationError(t, err, http.StatusBadRequest, "50 MB")
}

// zeroReader is an infinite reader of zero bytes, used to fill large tar entries
// in tests without allocating a large buffer.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestExtractAndMeasure_NotGzip(t *testing.T) {
	_, err := extractAndMeasure([]byte("not gzip"), t.TempDir())
	assertValidationError(t, err, http.StatusBadRequest, "invalid gzip")
}

func TestExtractAndMeasure_EmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	size, err := extractAndMeasure(buf.Bytes(), t.TempDir())
	require.NoError(t, err)
	assert.Zero(t, size)
}

func TestExtractAndMeasure_FileContentCorrect(t *testing.T) {
	const wantContent = "hello from template\n"
	archive := buildArchive(t, map[string]string{
		"podman/templates/svc.yaml.tmpl": wantContent,
	}, true)

	destDir := t.TempDir()
	_, err := extractAndMeasure(archive, destDir)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(destDir, "podman", "templates", "svc.yaml.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, wantContent, string(got))
}

// -----------------------------------------------------------------------
// ServiceMetadata / ComponentMetadata
// -----------------------------------------------------------------------

func TestServiceMetadata_Fields(t *testing.T) {
	m := &bundlemetadata.ServiceMetadata{ID: "svc", Type: bundlemetadata.CatalogTypeService, Ver: "1.2.3", DisplayName: "My Svc"}
	assert.Equal(t, "svc", m.ID)
	assert.Equal(t, "1.2.3", m.Ver)
	assert.Equal(t, "My Svc", m.DisplayName)
}

func TestComponentMetadata_Fields(t *testing.T) {
	m := &bundlemetadata.ComponentMetadata{ID: "prov", Type: bundlemetadata.CatalogTypeComponent, ComponentType: "reranker", Ver: "2.0.0", DisplayName: "Re-rank"}
	assert.Equal(t, "reranker--prov", m.ComponentType+"--"+m.ID)
	assert.Equal(t, "reranker", m.ComponentType)
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// assertValidationError is a shared helper that asserts err is a *validators.ValidationError
// with the given HTTP code. If msgContains is non-empty it also checks the message.
func assertValidationError(t *testing.T, err error, code int, msgContains string) {
	t.Helper()
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	if msgContains != "" {
		assert.Contains(t, valErr.Message, msgContains)
	}
}

// assertFileExists asserts that a regular file exists at path.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err, "expected file to exist: %s", path)
	assert.False(t, info.IsDir(), "expected a file, got a directory: %s", path)
}
