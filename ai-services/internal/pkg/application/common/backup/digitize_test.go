package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDigitizeFilesNamesFilesAfterID(t *testing.T) {
	dir := t.TempDir()

	err := writeDigitizeFiles(dir, "doc_id", "_checksum.json", "document checksum", []map[string]interface{}{
		{"doc_id": "doc-1", "checksum": "sum-1"},
		{"doc_id": "doc-2", "checksum": "sum-2"},
	})

	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "doc-1_checksum.json"))
	assert.FileExists(t, filepath.Join(dir, "doc-2_checksum.json"))

	content, err := os.ReadFile(filepath.Join(dir, "doc-1_checksum.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"checksum": "sum-1"`)
}

func TestWriteDigitizeFilesRejectsMissingOrNonStringID(t *testing.T) {
	for name, record := range map[string]map[string]interface{}{
		"missing":    {"name": "a.pdf"},
		"empty":      {"id": ""},
		"not-string": {"id": 42},
	} {
		t.Run(name, func(t *testing.T) {
			err := writeDigitizeFiles(t.TempDir(), "id", "_metadata.json", "document", []map[string]interface{}{record})

			require.Error(t, err)
			assert.Equal(t, "export response contains document without valid id", err.Error())
		})
	}
}

func TestCreateDigitizeBackupArchiveEmptyExportStillCreatesLayout(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "empty.tar.gz")

	require.NoError(t, CreateDigitizeBackupArchive(archive, &DigitizeExportResponse{}))
	assert.FileExists(t, archive)
}
