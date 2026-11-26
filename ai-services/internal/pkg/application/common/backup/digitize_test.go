package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/utils"
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

type fakeFetcher struct {
	content map[string]*DigitizeContentResponse
	calls   []string
}

func (f *fakeFetcher) FetchDocumentContent(_ context.Context, docID string) (*DigitizeContentResponse, error) {
	f.calls = append(f.calls, docID)

	content, ok := f.content[docID]
	if !ok {
		return nil, ErrContentNotFound
	}

	return content, nil
}

func sampleExport() *DigitizeExportResponse {
	return &DigitizeExportResponse{
		Data: DigitizeImportExportData{
			Jobs: []map[string]interface{}{{"job_id": "job-1"}, {"job_id": "job-2"}},
			Documents: []map[string]interface{}{
				{"id": "ing-1", "type": "ingestion", "status": "completed"},
				{"id": "dig-json", "type": "digitization", "status": "completed"},
				{"id": "dig-md", "type": "digitization", "status": "completed"},
				{"id": "dig-failed", "type": "digitization", "status": "failed"},
				{"id": "dig-gone", "type": "digitization", "status": "completed"},
			},
			DocumentChecksums: []map[string]interface{}{{"checksum": "h1", "type": "ingestion", "doc_id": "ing-1"}},
		},
	}
}

func extract(t *testing.T, archive string) string {
	t.Helper()

	dir := t.TempDir()
	if err := utils.ExtractTarGz(archive, dir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	return filepath.Join(dir, "backup")
}

func TestCreateDigitizeBackupArchiveEmptyExportStillCreatesLayout(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "empty.tar.gz")

	require.NoError(t, CreateDigitizeBackupArchive(context.Background(), archive, &DigitizeExportResponse{}, nil))
	assert.FileExists(t, archive)
}

func TestCreateDigitizeBackupArchive_WithoutFetcher_HasNoContent(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "b.tar.gz")

	if err := CreateDigitizeBackupArchive(context.Background(), archive, sampleExport(), nil); err != nil {
		t.Fatalf("CreateDigitizeBackupArchive: %v", err)
	}

	backupDir := extract(t, archive)

	if _, err := os.Stat(filepath.Join(backupDir, "cache", DigitizeContentDir)); !os.IsNotExist(err) {
		t.Fatalf("digitized content dir must not exist without fetcher, stat err = %v", err)
	}

	if _, err := os.Stat(filepath.Join(backupDir, "cache", "checksums", "ing-1_checksum.json")); err != nil {
		t.Fatalf("checksums file missing: %v", err)
	}

	info := readJSONMap(t, filepath.Join(backupDir, "backup_info.json"))
	if info["includes_digitized_content"] != false {
		t.Fatalf("includes_digitized_content = %v, want false", info["includes_digitized_content"])
	}
}

func TestCreateDigitizeBackupArchive_WithFetcher_WritesCompletedDigitizationContent(t *testing.T) {
	fetcher := &fakeFetcher{content: map[string]*DigitizeContentResponse{
		"dig-json": {Result: json.RawMessage(`{"pages":[1]}`), OutputFormat: "json"},
		"dig-md":   {Result: json.RawMessage(`"# title\nbody"`), OutputFormat: "md"},
	}}
	archive := filepath.Join(t.TempDir(), "b.tar.gz")

	if err := CreateDigitizeBackupArchive(context.Background(), archive, sampleExport(), fetcher); err != nil {
		t.Fatalf("CreateDigitizeBackupArchive: %v", err)
	}

	contentDir := filepath.Join(extract(t, archive), "cache", DigitizeContentDir)

	if got := readFile(t, filepath.Join(contentDir, "dig-json.json")); got != `{"pages":[1]}` {
		t.Fatalf("dig-json.json = %q", got)
	}

	if got := readFile(t, filepath.Join(contentDir, "dig-md.md")); got != "# title\nbody" {
		t.Fatalf("dig-md.md = %q", got)
	}

	entries, err := os.ReadDir(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 content files, got %d", len(entries))
	}

	// Only completed digitization documents are fetched: not ingestion, not failed.
	want := map[string]bool{"dig-json": true, "dig-md": true, "dig-gone": true}
	if len(fetcher.calls) != len(want) {
		t.Fatalf("fetched %v, want keys of %v", fetcher.calls, want)
	}

	for _, id := range fetcher.calls {
		if !want[id] {
			t.Fatalf("unexpected fetch for %q", id)
		}
	}
}

func TestWriteDigitizedContentFiles_RejectsPathTraversalID(t *testing.T) {
	docs := []map[string]interface{}{{"id": "../evil", "type": "digitization", "status": "completed"}}
	fetcher := &fakeFetcher{content: map[string]*DigitizeContentResponse{}}

	if err := writeDigitizedContentFiles(context.Background(), t.TempDir(), fetcher, docs); err == nil {
		t.Fatal("expected error for traversal doc id")
	}

	if len(fetcher.calls) != 0 {
		t.Fatalf("must not fetch content for invalid id, calls = %v", fetcher.calls)
	}
}

func TestCallExportAPI_ExportsAllRecordsWithContent(t *testing.T) {
	var gotQuery url.Values

	var contentRequested bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/export" {
			gotQuery = r.URL.Query()

			_ = json.NewEncoder(w).Encode(sampleExport())

			return
		}

		contentRequested = true

		_ = json.NewEncoder(w).Encode(DigitizeContentResponse{Result: json.RawMessage(`{}`), OutputFormat: "json"})
	}))
	defer srv.Close()

	client := &DigitizeBackupClient{client: resty.New().SetBaseURL(srv.URL)}

	resp, err := client.CallExportAPI(context.Background())
	if err != nil {
		t.Fatalf("CallExportAPI: %v", err)
	}

	if err := CreateDigitizeBackupArchive(context.Background(), filepath.Join(t.TempDir(), "b.tar.gz"), resp, client); err != nil {
		t.Fatalf("CreateDigitizeBackupArchive: %v", err)
	}

	if gotQuery.Has("operation") {
		t.Fatalf("export request must not carry an operation param, got %q", gotQuery.Get("operation"))
	}

	if !contentRequested {
		t.Fatal("digitized content was not fetched")
	}
}

func TestFetchDocumentContent_NotFoundMapsToSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := &DigitizeBackupClient{client: resty.New().SetBaseURL(srv.URL)}

	if _, err := client.FetchDocumentContent(context.Background(), "x"); err != ErrContentNotFound {
		t.Fatalf("err = %v, want ErrContentNotFound", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func readJSONMap(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(readFile(t, path)), &m); err != nil {
		t.Fatal(err)
	}

	return m
}
