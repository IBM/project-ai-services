package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonbackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
)

func exportResponse(jobs, docs, checksums []map[string]interface{}) *commonbackup.DigitizeExportResponse {
	return &commonbackup.DigitizeExportResponse{
		Status: "completed",
		Data: commonbackup.DigitizeImportExportData{
			Jobs:              jobs,
			Documents:         docs,
			DocumentChecksums: checksums,
		},
	}
}

// payloadData returns payload["data"] as a map.
func payloadData(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()

	data, ok := payload["data"].(map[string]interface{})
	require.True(t, ok, "payload must have a data object")

	return data
}

func TestDigitizeBackupRestoreRoundTripIncludesDocumentChecksums(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "digitize.tar.gz")

	err := commonbackup.CreateDigitizeBackupArchive(archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1", "operation": "ingestion"}},
		[]map[string]interface{}{{"id": "doc-1", "job_id": "job-1", "name": "a.pdf"}},
		[]map[string]interface{}{
			{"checksum": "sum-1", "type": "ingestion", "doc_id": "doc-1"},
			{"checksum": "sum-2", "type": "digitization", "doc_id": "doc-2"},
		},
	))
	require.NoError(t, err)

	payload, err := GetDigitizeData(archive)
	require.NoError(t, err)

	data := payloadData(t, payload)
	assert.Len(t, data["jobs"], 1)
	assert.Len(t, data["documents"], 1)

	checksums, ok := data["document_checksums"].([]interface{})
	require.True(t, ok)
	require.Len(t, checksums, 2)

	byDoc := map[string]map[string]interface{}{}
	for _, c := range checksums {
		m := c.(map[string]interface{})
		byDoc[m["doc_id"].(string)] = m
	}

	assert.Equal(t, "sum-1", byDoc["doc-1"]["checksum"])
	assert.Equal(t, "ingestion", byDoc["doc-1"]["type"])
	assert.Equal(t, "sum-2", byDoc["doc-2"]["checksum"])
	assert.Equal(t, "digitization", byDoc["doc-2"]["type"])
}

func TestDigitizeBackupWithoutChecksumsRestoresAsEmptyList(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "digitize.tar.gz")

	require.NoError(t, commonbackup.CreateDigitizeBackupArchive(archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1"}}, nil, nil,
	)))

	payload, err := GetDigitizeData(archive)
	require.NoError(t, err)

	checksums, ok := payloadData(t, payload)["document_checksums"].([]interface{})
	require.True(t, ok, "document_checksums must be [] and not null in the import payload")
	assert.Empty(t, checksums)
}

func TestCreateDigitizeBackupArchiveRejectsChecksumWithoutDocID(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "digitize.tar.gz")

	err := commonbackup.CreateDigitizeBackupArchive(archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1"}},
		nil,
		[]map[string]interface{}{{"checksum": "sum-1", "type": "ingestion"}},
	))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doc_id")
}

func TestConstructMetadataFromCacheOldBackupLayout(t *testing.T) {
	// A backup taken before checksums were exported has no cache/checksums directory.
	backupDir := t.TempDir()
	jobsDir := filepath.Join(backupDir, "cache", "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(jobsDir, "job-1_status.json"), []byte(`{"job_id":"job-1"}`), 0o644,
	))

	payload, err := constructMetadataFromCache(backupDir)
	require.NoError(t, err)

	data := payloadData(t, payload)
	assert.Len(t, data["jobs"], 1)

	// Missing directories must give [] (not null) so the import API accepts the payload.
	for _, key := range []string{"documents", "document_checksums"} {
		records, ok := data[key].([]interface{})
		require.True(t, ok, "%s must be a non-nil list", key)
		assert.NotNil(t, records)
		assert.Empty(t, records)
	}
}

func TestConstructMetadataFromCacheChecksumsOnlyIsValid(t *testing.T) {
	backupDir := t.TempDir()
	checksumsDir := filepath.Join(backupDir, "cache", "checksums")
	require.NoError(t, os.MkdirAll(checksumsDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(checksumsDir, "doc-1_checksum.json"),
		[]byte(`{"checksum":"sum-1","type":"ingestion","doc_id":"doc-1"}`), 0o644,
	))

	payload, err := constructMetadataFromCache(backupDir)
	require.NoError(t, err)
	assert.Len(t, payloadData(t, payload)["document_checksums"], 1)
}

func TestConstructMetadataFromCacheEmptyIsRejected(t *testing.T) {
	backupDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(backupDir, "cache"), 0o755))

	_, err := constructMetadataFromCache(backupDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no jobs, documents or document checksums")
}

func TestReadDigitizeFilesSkipsUnrelatedAndMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "doc-1_checksum.json"), []byte(`{"doc_id":"doc-1"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "doc-2_checksum.json"), []byte(`not json`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`{"doc_id":"x"}`), 0o644))

	checksums, err := readDigitizeFiles(dir, "_checksum.json", "document checksum")

	require.NoError(t, err)
	require.Len(t, checksums, 1)
	assert.Equal(t, "doc-1", checksums[0].(map[string]interface{})["doc_id"])
}
