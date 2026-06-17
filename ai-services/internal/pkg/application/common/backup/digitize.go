package common

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	ExportAllRecordsLimit = "-1"
	defaultFilePermission = 0o644
	defaultDirPermission  = 0o755
	bytesPerKB            = 1024
	bytesPerMB            = bytesPerKB * 1024
)

// DigitizeExportResponse represents the response from the digitize export API.
type DigitizeExportResponse struct {
	Status          string                   `json:"status"`
	Data            DigitizeImportExportData `json:"data"`
	Summary         DigitizeExportSummary    `json:"summary"`
	ExportTimestamp string                   `json:"export_timestamp"`
	DurationSeconds float64                  `json:"duration_seconds"`
	Pagination      DigitizeExportPagination `json:"pagination"`
}

// DigitizeImportExportData contains the jobs and documents data.
type DigitizeImportExportData struct {
	Jobs      []map[string]interface{} `json:"jobs"`
	Documents []map[string]interface{} `json:"documents"`
}

// DigitizeExportSummary contains summary information for the export.
type DigitizeExportSummary struct {
	Jobs      DigitizeExportEntitySummary `json:"jobs"`
	Documents DigitizeExportEntitySummary `json:"documents"`
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

	return &DigitizeBackupClient{
		client: client,
	}
}

// CallExportAPI calls the digitize Export API.
func (c *DigitizeBackupClient) CallExportAPI() (*DigitizeExportResponse, error) {
	logger.Infoln("Calling digitize Export API...")

	var exportResponse DigitizeExportResponse

	logger.Infoln("Sending export request to: /v1/export?limit=-1")
	resp, err := c.client.R().
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

// CreateDigitizeBackupArchive creates a backup archive from the export response.
func CreateDigitizeBackupArchive(backupFile string, exportResponse *DigitizeExportResponse) error {
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
	jobsDir := filepath.Join(cacheDir, "jobs")
	docsDir := filepath.Join(cacheDir, "docs")

	for _, dir := range []string{backupDir, cacheDir, jobsDir, docsDir} {
		if err := os.MkdirAll(dir, defaultDirPermission); err != nil {
			return fmt.Errorf("failed to create backup directory %s: %w", dir, err)
		}
	}

	if err := writeDigitizeJobFiles(jobsDir, exportResponse.Data.Jobs); err != nil {
		return err
	}

	if err := writeDigitizeDocumentFiles(docsDir, exportResponse.Data.Documents); err != nil {
		return err
	}

	if err := writeDigitizeBackupInfo(backupDir); err != nil {
		return err
	}

	if err := CreateTarGzArchive(tempDir, backupFile, []string{"backup"}); err != nil {
		return err
	}

	LogArchiveSize(backupFile)

	return nil
}

func writeDigitizeJobFiles(jobsDir string, jobs []map[string]interface{}) error {
	for _, job := range jobs {
		jobID, ok := job["job_id"].(string)
		if !ok || jobID == "" {
			return fmt.Errorf("export response contains job without valid job_id")
		}

		filePath := filepath.Join(jobsDir, fmt.Sprintf("%s_status.json", jobID))
		if err := writeJSONFile(filePath, job); err != nil {
			return fmt.Errorf("failed to write job file for %s: %w", jobID, err)
		}
	}

	return nil
}

func writeDigitizeDocumentFiles(docsDir string, documents []map[string]interface{}) error {
	for _, document := range documents {
		docID, ok := document["id"].(string)
		if !ok || docID == "" {
			return fmt.Errorf("export response contains document without valid id")
		}

		filePath := filepath.Join(docsDir, fmt.Sprintf("%s_metadata.json", docID))
		if err := writeJSONFile(filePath, document); err != nil {
			return fmt.Errorf("failed to write document file for %s: %w", docID, err)
		}
	}

	return nil
}

func writeDigitizeBackupInfo(tempDir string) error {
	backupInfo := map[string]interface{}{
		"backup_date": time.Now().Format(time.RFC3339),
		"type":        "digitize",
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

// Made with Bob
