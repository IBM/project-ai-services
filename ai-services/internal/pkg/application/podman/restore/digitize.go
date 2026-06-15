package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ConstructMetadataFromCache reads cache files and constructs the Import API payload.
func ConstructMetadataFromCache(backupDir string) (map[string]interface{}, error) {
	cacheDir := filepath.Join(backupDir, "cache")

	// Verify cache directory exists
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache directory not found in backup at: %s", cacheDir)
	}

	logger.Infof("Constructing metadata from cache files at: %s\n", cacheDir, 0)

	// Read job files
	jobs, err := readJobFiles(filepath.Join(cacheDir, "jobs"))
	if err != nil {
		return nil, fmt.Errorf("failed to read job files: %w", err)
	}

	// Read document files
	documents, err := readDocumentFiles(filepath.Join(cacheDir, "docs"))
	if err != nil {
		return nil, fmt.Errorf("failed to read document files: %w", err)
	}

	if len(jobs) == 0 && len(documents) == 0 {
		return nil, fmt.Errorf("no jobs or documents found in cache")
	}

	logger.Infof("Constructed metadata: %d job(s) and %d document(s)\n", len(jobs), len(documents), 0)

	// Construct the payload in Import API format
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"jobs":      jobs,
			"documents": documents,
		},
	}

	return payload, nil
}

// readJobFiles reads all job status JSON files from the jobs directory.
func readJobFiles(jobsDir string) ([]interface{}, error) {
	// Check if jobs directory exists
	if _, err := os.Stat(jobsDir); os.IsNotExist(err) {
		logger.Infof("No jobs directory found, skipping job import\n", 0)

		return nil, nil
	}

	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read jobs directory: %w", err)
	}

	jobs := make([]interface{}, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_status.json") {
			continue
		}

		filePath := filepath.Join(jobsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warningf("Failed to read job file %s: %v\n", entry.Name(), err)

			continue
		}

		var job map[string]interface{}
		if err := json.Unmarshal(data, &job); err != nil {
			logger.Warningf("Failed to parse job file %s: %v\n", entry.Name(), err)

			continue
		}

		jobs = append(jobs, job)
	}

	logger.Infof("Read %d job(s) from cache\n", len(jobs), 0)

	return jobs, nil
}

// readDocumentFiles reads all document metadata JSON files from the docs directory.
func readDocumentFiles(docsDir string) ([]interface{}, error) {
	// Check if docs directory exists
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		logger.Infof("No docs directory found, skipping document import\n", 0)

		return nil, nil
	}

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read docs directory: %w", err)
	}

	documents := make([]interface{}, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_metadata.json") {
			continue
		}

		filePath := filepath.Join(docsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warningf("Failed to read document file %s: %v\n", entry.Name(), err)

			continue
		}

		var doc map[string]interface{}
		if err := json.Unmarshal(data, &doc); err != nil {
			logger.Warningf("Failed to parse document file %s: %v\n", entry.Name(), err)

			continue
		}

		documents = append(documents, doc)
	}

	logger.Infof("Read %d document(s) from cache\n", len(documents), 0)

	return documents, nil
}

// DigitizeRestoreClient wraps the HTTP client for digitize restore operations.
type DigitizeRestoreClient struct {
	client *resty.Client
}

// NewDigitizeRestoreClient creates a new digitize restore client.
func NewDigitizeRestoreClient(serviceURL string) *DigitizeRestoreClient {
	return &DigitizeRestoreClient{
		client: resty.New().SetBaseURL(serviceURL),
	}
}

// GetDigitizeAPIURL extracts the digitize API URL from application details.
func GetDigitizeAPIURL(appDetails *catalogTypes.Application) (string, error) {
	// Search through services to find digitize service
	for _, service := range appDetails.Services {
		if service.Type == "digitize" {
			// Look for API endpoint
			for _, endpoint := range service.Endpoints {
				if endpointType, ok := endpoint["type"].(string); ok && endpointType == "api" {
					if url, ok := endpoint["url"].(string); ok && url != "" {
						return url, nil
					}
				}
			}

			return "", fmt.Errorf("digitize service found but no API endpoint available")
		}
	}

	return "", fmt.Errorf("digitize service not found in application")
}

// CallImportAPI calls the digitize service Import API with the metadata payload.
func (c *DigitizeRestoreClient) CallImportAPI(payload map[string]interface{}) error {
	logger.Infof("Calling digitize Import API...\n", 0)

	// Prepare response container
	var importResponse map[string]interface{}

	// Make the API call using the reusable HTTP client
	logger.Infof("Sending import request to: /v1/import\n", 0)
	resp, err := c.client.R().
		SetBody(payload).
		SetResult(&importResponse).
		Post("/v1/import")

	if err != nil {
		return fmt.Errorf("failed to call import API: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("import API returned HTTP %d: %s", resp.StatusCode(), resp.String())
	}

	// Log import results
	logImportSummary(importResponse)
	logImportErrors(importResponse)
	logImportWarnings(importResponse)

	return nil
}

func logImportSummary(importResponse map[string]interface{}) {
	summary, ok := importResponse["summary"].(map[string]interface{})
	if !ok {
		return
	}

	logger.Infof("Import summary:\n", 0)

	if jobs, ok := summary["jobs"].(map[string]interface{}); ok {
		logger.Infof("  Jobs - imported: %d, skipped: %d, failed: %d\n",
			utils.GetNumericValFromMap(jobs, "imported"), utils.GetNumericValFromMap(jobs, "skipped"), utils.GetNumericValFromMap(jobs, "failed"), 0)
	}

	if docs, ok := summary["documents"].(map[string]interface{}); ok {
		logger.Infof("  Documents - imported: %d, skipped: %d, failed: %d\n",
			utils.GetNumericValFromMap(docs, "imported"), utils.GetNumericValFromMap(docs, "skipped"), utils.GetNumericValFromMap(docs, "failed"), 0)
	}
}

func logImportErrors(importResponse map[string]interface{}) {
	errors, ok := importResponse["errors"].([]interface{})
	if !ok || len(errors) == 0 {
		return
	}

	logger.Warningf("Import completed with %d error(s)\n", len(errors))

	for i, err := range errors {
		if errMap, ok := err.(map[string]interface{}); ok {
			logger.Warningf("  Error %d: %v\n", i+1, errMap["message"])
		}
	}
}

func logImportWarnings(importResponse map[string]interface{}) {
	warnings, ok := importResponse["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		return
	}

	logger.Infof("Import completed with %d warning(s)\n", len(warnings), 0)
}

// Made with Bob
