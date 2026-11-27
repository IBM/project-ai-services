package common

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	ExportAllRecordsLimit = "-1"
	defaultFilePermission = 0o644
	defaultDirPermission  = 0o755
	bytesPerKB            = 1024
	bytesPerMB            = bytesPerKB * 1024

	// DigitizeContentDir is the archive directory (under cache/) holding digitized output files.
	DigitizeContentDir = "digitized"

	operationDigitization = "digitization"
	statusCompleted       = "completed"
)

// ErrContentNotFound is returned when the digitize service has no content file for a document.
var ErrContentNotFound = errors.New("digitized content not found")

// DigitizeExportResponse represents the response from the digitize export API.
type DigitizeExportResponse struct {
	Status          string                   `json:"status"`
	Data            DigitizeImportExportData `json:"data"`
	Summary         DigitizeExportSummary    `json:"summary"`
	ExportTimestamp string                   `json:"export_timestamp"`
	DurationSeconds float64                  `json:"duration_seconds"`
	Pagination      DigitizeExportPagination `json:"pagination"`
}

// DigitizeImportExportData contains the jobs, documents and document checksums data.
type DigitizeImportExportData struct {
	Jobs              []map[string]interface{} `json:"jobs"`
	Documents         []map[string]interface{} `json:"documents"`
	DocumentChecksums []map[string]interface{} `json:"document_checksums"`
}

// DigitizeExportSummary contains summary information for the export.
type DigitizeExportSummary struct {
	Jobs              DigitizeExportEntitySummary `json:"jobs"`
	Documents         DigitizeExportEntitySummary `json:"documents"`
	DocumentChecksums DigitizeExportEntitySummary `json:"document_checksums"`
}

// DigitizeContentResponse is the response of GET /v1/documents/{id}/content.
type DigitizeContentResponse struct {
	Result       json.RawMessage `json:"result"`
	OutputFormat string          `json:"output_format"`
}

// ContentFetcher retrieves the digitized content of a document.
type ContentFetcher interface {
	FetchDocumentContent(ctx context.Context, docID string) (*DigitizeContentResponse, error)
}

// DigitizeExportEntitySummary contains summary for a specific entity type.
type DigitizeExportEntitySummary struct {
	TotalExported int `json:"total_exported"`
	Completed     int `json:"completed"`
	Failed        int `json:"failed"`
}

// DigitizeExportPagination contains pagination information.
type DigitizeExportPagination struct {
	Limit           int  `json:"limit"`
	Offset          int  `json:"offset"`
	HasMore         bool `json:"has_more"`
	TotalRecords    int  `json:"total_records"`
	ReturnedRecords int  `json:"returned_records"`
}

// DigitizeBackupClient wraps the HTTP client for digitize backup operations.
type DigitizeBackupClient struct {
	client *resty.Client
}

// NewDigitizeBackupClient creates a new digitize backup client.
func NewDigitizeBackupClient(serviceURL string) *DigitizeBackupClient {
	client := resty.New().SetBaseURL(serviceURL)

	// Check runtime type
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	switch runtimeType {
	case runtimeTypes.RuntimeTypeOpenShift:
		// For OpenShift, always skip TLS verification
		client.SetTLSClientConfig(&tls.Config{
			InsecureSkipVerify: true,
		})
	case runtimeTypes.RuntimeTypePodman:
		// For Podman, check the insecure flag from catalog credentials
		creds, err := config.Load()
		if err == nil && creds.Insecure {
			client.SetTLSClientConfig(&tls.Config{
				InsecureSkipVerify: true,
			})
		}
	}

	return &DigitizeBackupClient{
		client: client,
	}
}

// CallExportAPI calls the digitize Export API, which returns the jobs, documents and
// checksums of both ingestion and digitization.
func (c *DigitizeBackupClient) CallExportAPI(ctx context.Context) (*DigitizeExportResponse, error) {
	logger.Infoln("Calling digitize Export API...")

	var exportResponse DigitizeExportResponse

	logger.Infoln("Sending export request to: /v1/export?limit=-1")
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("limit", ExportAllRecordsLimit).
		SetResult(&exportResponse).
		Get("/v1/export")
	if err != nil {
		return nil, fmt.Errorf("failed to call export API: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("export API returned HTTP %d: %s", resp.StatusCode(), resp.String())
	}

	return &exportResponse, nil
}

// FetchDocumentContent downloads the digitized content of a document.
func (c *DigitizeBackupClient) FetchDocumentContent(ctx context.Context, docID string) (*DigitizeContentResponse, error) {
	var content DigitizeContentResponse

	resp, err := c.client.R().
		SetContext(ctx).
		SetPathParam("doc_id", docID).
		SetResult(&content).
		Get("/v1/documents/{doc_id}/content")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch content for document %s: %w", docID, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, ErrContentNotFound
	}

	if resp.IsError() {
		return nil, fmt.Errorf("content API for document %s returned HTTP %d: %s", docID, resp.StatusCode(), resp.String())
	}

	return &content, nil
}

// BackupDigitize exports digitize metadata and writes it to backupFile.
//
// The ingestion and digitization jobs, documents and checksums are backed up,
// together with the digitized output files (json/md/txt) of every completed
// digitization document.
func BackupDigitize(ctx context.Context, digitizeURL, backupFile string) (*DigitizeExportResponse, error) {
	client := NewDigitizeBackupClient(digitizeURL)

	exportResponse, err := client.CallExportAPI(ctx)
	if err != nil {
		return nil, err
	}

	if err := CreateDigitizeBackupArchive(ctx, backupFile, exportResponse, client); err != nil {
		return nil, err
	}

	return exportResponse, nil
}

// CreateDigitizeBackupArchive creates a backup archive from the export response.
// When fetcher is non-nil, the digitized content of every completed digitization
// document is downloaded and stored under cache/digitized.
func CreateDigitizeBackupArchive(ctx context.Context, backupFile string, exportResponse *DigitizeExportResponse, fetcher ContentFetcher) error {
	logger.Infoln("Creating digitize backup archive...")

	tempDir, err := os.MkdirTemp("", "digitize-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Warningf("Failed to remove temp directory: %v\n", err)
		}
	}()

	// Create backup directory structure to match restore expectations
	backupDir := filepath.Join(tempDir, "backup")
	cacheDir := filepath.Join(backupDir, "cache")

	// One cache sub-directory per record type; the restore side reads the same layout.
	digitizeFiles := []struct {
		subDir, idKey, suffix, kind string
		records                     []map[string]interface{}
	}{
		{"jobs", "job_id", "_status.json", "job", exportResponse.Data.Jobs},
		{"docs", "id", "_metadata.json", "document", exportResponse.Data.Documents},
		// Named after doc_id, which is unique in the document_checksum registry.
		{"checksums", "doc_id", "_checksum.json", "document checksum", exportResponse.Data.DocumentChecksums},
	}

	for _, c := range digitizeFiles {
		// MkdirAll also creates backup/ and backup/cache/, even when a record list is empty.
		dir := filepath.Join(cacheDir, c.subDir)
		if err := os.MkdirAll(dir, defaultDirPermission); err != nil {
			return fmt.Errorf("failed to create backup directory %s: %w", dir, err)
		}

		if err := writeDigitizeFiles(dir, c.idKey, c.suffix, c.kind, c.records); err != nil {
			return err
		}
	}

	if fetcher != nil {
		if err := writeDigitizedContentFiles(ctx, filepath.Join(cacheDir, DigitizeContentDir), fetcher, exportResponse.Data.Documents); err != nil {
			return err
		}
	}

	if err := writeDigitizeBackupInfo(backupDir, fetcher != nil); err != nil {
		return err
	}

	if err := CreateTarGzArchive(tempDir, backupFile, []string{"backup"}); err != nil {
		return err
	}

	LogArchiveSize(backupFile)

	return nil
}

// writeDigitizeFiles writes each record to dir as <record[idKey]><suffix>. kind names the
// record type ("job", "document", ...) in errors. A record without a string idKey fails
// the backup, because it could not be restored.
func writeDigitizeFiles(dir, idKey, suffix, kind string, records []map[string]interface{}) error {
	for _, record := range records {
		id, ok := record[idKey].(string)
		if !ok || id == "" {
			return fmt.Errorf("export response contains %s without valid %s", kind, idKey)
		}

		if err := writeJSONFile(filepath.Join(dir, id+suffix), record); err != nil {
			return fmt.Errorf("failed to write %s file for %s: %w", kind, id, err)
		}
	}

	return nil
}

// writeDigitizedContentFiles downloads the content of every completed digitization
// document into contentDir, one file per document, writing each to disk before fetching the next.
func writeDigitizedContentFiles(ctx context.Context, contentDir string, fetcher ContentFetcher, documents []map[string]interface{}) error {
	var written, missing int

	for _, document := range documents {
		if !isCompletedDigitizationDocument(document) {
			continue
		}

		docID, _ := document["id"].(string)
		if docID == "" || docID != filepath.Base(docID) {
			return fmt.Errorf("export response contains document with invalid id %q", docID)
		}

		content, err := fetcher.FetchDocumentContent(ctx, docID)
		if errors.Is(err, ErrContentNotFound) {
			logger.Warningf("Digitized content for document %s not found, skipping\n", docID)

			missing++

			continue
		}

		if err != nil {
			return err
		}

		if err := writeDigitizedContentFile(contentDir, docID, content); err != nil {
			return err
		}

		written++
	}

	logger.Infof("Digitized content - backed up: %d, missing: %d\n", written, missing)

	return nil
}

func isCompletedDigitizationDocument(document map[string]interface{}) bool {
	docType, _ := document["type"].(string)
	status, _ := document["status"].(string)

	return docType == operationDigitization && status == statusCompleted
}

func writeDigitizedContentFile(contentDir, docID string, content *DigitizeContentResponse) error {
	var data []byte

	switch content.OutputFormat {
	case "json":
		data = content.Result
	case "md", "txt":
		var text string
		if err := json.Unmarshal(content.Result, &text); err != nil {
			return fmt.Errorf("content of document %s is not text: %w", docID, err)
		}

		data = []byte(text)
	default:
		return fmt.Errorf("document %s has unsupported output format %q", docID, content.OutputFormat)
	}

	if err := os.MkdirAll(contentDir, defaultDirPermission); err != nil {
		return fmt.Errorf("failed to create digitized content directory: %w", err)
	}

	filePath := filepath.Join(contentDir, fmt.Sprintf("%s.%s", docID, content.OutputFormat))
	if err := os.WriteFile(filePath, data, defaultFilePermission); err != nil {
		return fmt.Errorf("failed to write content file for %s: %w", docID, err)
	}

	return nil
}

func writeDigitizeBackupInfo(tempDir string, includesDigitizedContent bool) error {
	backupInfo := map[string]interface{}{
		"backup_date":                time.Now().Format(time.RFC3339),
		"type":                       "digitize",
		"includes_digitized_content": includesDigitizedContent,
	}

	return writeJSONFile(filepath.Join(tempDir, "backup_info.json"), backupInfo)
}

func writeJSONFile(path string, data interface{}) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for %s: %w", path, err)
	}

	if err := os.WriteFile(path, append(content, '\n'), defaultFilePermission); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

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

func GetBackupFile(backupFile string, appName string) (string, error) {
	if backupFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		backupFile = fmt.Sprintf("%s_digitize_backup_%s.tar.gz", appName, timestamp)
	}

	// Ensure .tar.gz extension
	if !strings.HasSuffix(backupFile, ".tar.gz") {
		backupFile += ".tar.gz"
	}

	// Get absolute path for backup file
	absBackupFile, err := filepath.Abs(backupFile)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	return absBackupFile, nil
}
