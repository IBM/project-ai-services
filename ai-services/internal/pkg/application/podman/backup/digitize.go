package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	exportAllRecordsLimit = "-1"
)

type DigitizeExportResponse struct {
	Status          string                   `json:"status"`
	Data            DigitizeImportExportData `json:"data"`
	Summary         DigitizeExportSummary    `json:"summary"`
	ExportTimestamp string                   `json:"export_timestamp"`
	DurationSeconds float64                  `json:"duration_seconds"`
	Pagination      DigitizeExportPagination `json:"pagination"`
}

type DigitizeImportExportData struct {
	Jobs      []map[string]interface{} `json:"jobs"`
	Documents []map[string]interface{} `json:"documents"`
}

type DigitizeExportSummary struct {
	Jobs      DigitizeExportEntitySummary `json:"jobs"`
	Documents DigitizeExportEntitySummary `json:"documents"`
}

type DigitizeExportEntitySummary struct {
	TotalExported int `json:"total_exported"`
	Completed     int `json:"completed"`
	Failed        int `json:"failed"`
}

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
	return &DigitizeBackupClient{
		client: resty.New().SetBaseURL(serviceURL),
	}
}

// CallExportAPI calls the digitize Export API.
func (c *DigitizeBackupClient) CallExportAPI() (*DigitizeExportResponse, error) {
	logger.Infof("Calling digitize Export API...\n", 0)

	var exportResponse DigitizeExportResponse

	logger.Infof("Sending export request to: /v1/export?limit=-1\n", 0)
	resp, err := c.client.R().
		SetQueryParam("limit", exportAllRecordsLimit).
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

func CreateDigitizeBackupArchive(backupFile string, exportResponse *DigitizeExportResponse) error {
	logger.Infof("Creating digitize backup archive...\n", 0)

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

// Made with Bob
