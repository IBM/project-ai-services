package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonbackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

	err := commonbackup.CreateDigitizeBackupArchive(context.Background(), archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1", "operation": "ingestion"}},
		[]map[string]interface{}{{"id": "doc-1", "job_id": "job-1", "name": "a.pdf"}},
		[]map[string]interface{}{
			{"checksum": "sum-1", "type": "ingestion", "doc_id": "doc-1"},
			{"checksum": "sum-2", "type": "digitization", "doc_id": "doc-2"},
		},
	), nil)
	require.NoError(t, err)

	payload, err := constructMetadataFromCache(extractBackup(t, archive))
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

	require.NoError(t, commonbackup.CreateDigitizeBackupArchive(context.Background(), archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1"}}, nil, nil,
	), nil))

	payload, err := constructMetadataFromCache(extractBackup(t, archive))
	require.NoError(t, err)

	checksums, ok := payloadData(t, payload)["document_checksums"].([]interface{})
	require.True(t, ok, "document_checksums must be [] and not null in the import payload")
	assert.Empty(t, checksums)
}

func TestCreateDigitizeBackupArchiveRejectsChecksumWithoutDocID(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "digitize.tar.gz")

	err := commonbackup.CreateDigitizeBackupArchive(context.Background(), archive, exportResponse(
		[]map[string]interface{}{{"job_id": "job-1"}},
		nil,
		[]map[string]interface{}{{"checksum": "sum-1", "type": "ingestion"}},
	), nil)

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

func TestListDigitizedContentFiles(t *testing.T) {
	backupDir := t.TempDir()

	files, err := listDigitizedContentFiles(backupDir)
	if err != nil || files != nil {
		t.Fatalf("no content dir: files=%v err=%v, want nil,nil", files, err)
	}

	dir := filepath.Join(backupDir, "cache", contentDir)
	writeFile(t, filepath.Join(dir, "a.json"), `{}`)
	writeFile(t, filepath.Join(dir, "b.md"), `# b`)
	writeFile(t, filepath.Join(dir, "c.txt"), `c`)
	writeFile(t, filepath.Join(dir, "ignored.pdf"), `x`)
	writeFile(t, filepath.Join(dir, ".tmp"), `x`)

	files, err = listDigitizedContentFiles(backupDir)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.docID+"."+f.format)
	}

	sort.Strings(got)

	want := []string{"a.json", "b.md", "c.txt"}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files = %v, want %v", got, want)
		}
	}
}

func TestCallImportContentAPI_EncodesPerFormat(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		content    string
		format     string
		wantResult string
	}{
		{name: "json is sent as object", file: "d1.json", content: `{"pages":[1]}`, format: "json", wantResult: `{"pages":[1]}`},
		{name: "markdown is sent as string", file: "d2.md", content: "# t\nb", format: "md", wantResult: `"# t\nb"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string

			var gotBody struct {
				Result       json.RawMessage `json:"result"`
				OutputFormat string          `json:"output_format"`
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "imported"})
			}))
			defer srv.Close()

			path := filepath.Join(t.TempDir(), tt.file)
			writeFile(t, path, tt.content)

			client := &DigitizeRestoreClient{client: resty.New().SetBaseURL(srv.URL)}

			written, err := client.CallImportContentAPI(context.Background(), digitizedContentFile{docID: "doc", format: tt.format, path: path})
			if err != nil {
				t.Fatalf("CallImportContentAPI: %v", err)
			}

			if !written {
				t.Fatal("written = false, want true")
			}

			if gotPath != "/v1/import/content/doc" {
				t.Fatalf("path = %q", gotPath)
			}

			if gotBody.OutputFormat != tt.format {
				t.Fatalf("output_format = %q, want %q", gotBody.OutputFormat, tt.format)
			}

			if string(gotBody.Result) != tt.wantResult {
				t.Fatalf("result = %s, want %s", gotBody.Result, tt.wantResult)
			}
		})
	}
}

func TestRestoreDigitizedContent_CountsSkippedAndFailsOnError(t *testing.T) {
	statuses := map[string]string{"new": "imported", "old": "skipped"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		id := filepath.Base(r.URL.Path)
		if id == "boom" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"status": statuses[id]})
	}))
	defer srv.Close()

	client := &DigitizeRestoreClient{client: resty.New().SetBaseURL(srv.URL)}
	dir := t.TempDir()

	mk := func(id string) digitizedContentFile {
		p := filepath.Join(dir, id+".json")
		writeFile(t, p, `{}`)

		return digitizedContentFile{docID: id, format: "json", path: p}
	}

	if err := restoreDigitizedContent(context.Background(), client, []digitizedContentFile{mk("new"), mk("old")}); err != nil {
		t.Fatalf("restoreDigitizedContent: %v", err)
	}

	if err := restoreDigitizedContent(context.Background(), client, []digitizedContentFile{mk("boom")}); err == nil {
		t.Fatal("expected error when the service rejects a content file")
	}

	if err := restoreDigitizedContent(context.Background(), client, nil); err != nil {
		t.Fatalf("no files must be a no-op, got %v", err)
	}
}

func extractBackup(t *testing.T, archive string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, utils.ExtractTarGz(archive, dir))

	return filepath.Join(dir, "backup")
}
