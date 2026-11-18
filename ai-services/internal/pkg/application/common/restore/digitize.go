package common

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
	podmanrestore "github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	contentDir = "digitized"
)

// ConstructMetadataFromCache reads cache files and constructs the Import API payload.
func constructMetadataFromCache(backupDir string) (map[string]interface{}, error) {
	cacheDir := filepath.Join(backupDir, "cache")

	// Verify cache directory exists
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache directory not found in backup at: %s", cacheDir)
	}

	logger.Infof("Constructing metadata from cache files at: %s\n", cacheDir)

	jobs, err := readDigitizeFiles(filepath.Join(cacheDir, "jobs"), "_status.json", "job")
	if err != nil {
		return nil, err
	}

	documents, err := readDigitizeFiles(filepath.Join(cacheDir, "docs"), "_metadata.json", "document")
	if err != nil {
		return nil, err
	}

	// The checksums directory is absent in backups taken before checksums were exported.
	documentChecksums, err := readDigitizeFiles(filepath.Join(cacheDir, "checksums"), "_checksum.json", "document checksum")
	if err != nil {
		return nil, err
	}

	if len(jobs) == 0 && len(documents) == 0 && len(documentChecksums) == 0 {
		return nil, fmt.Errorf("no jobs, documents or document checksums found in cache")
	}

	logger.Infof("Constructed metadata: %d job(s), %d document(s) and %d document checksum(s)\n",
		len(jobs), len(documents), len(documentChecksums))

	// Construct the payload in Import API format. The import API orders the records
	// by dependency itself (jobs, documents, document_checksums).
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"jobs":               jobs,
			"documents":          documents,
			"document_checksums": documentChecksums,
		},
	}

	return payload, nil
}

// digitizedContentFile is a digitized output file found in a backup.
type digitizedContentFile struct {
	docID  string
	format string
	path   string
}

// listDigitizedContentFiles returns the digitized output files (json/md/txt) in the backup, if any.
func listDigitizedContentFiles(backupDir string) ([]digitizedContentFile, error) {
	dir := filepath.Join(backupDir, "cache", contentDir)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read digitized content directory: %w", err)
	}

	files := make([]digitizedContentFile, 0, len(entries))

	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		format := strings.TrimPrefix(ext, ".")

		if entry.IsDir() || (format != "json" && format != "md" && format != "txt") {
			continue
		}

		files = append(files, digitizedContentFile{
			docID:  strings.TrimSuffix(entry.Name(), ext),
			format: format,
			path:   filepath.Join(dir, entry.Name()),
		})
	}

	return files, nil
}

// readDigitizeFiles reads every JSON file in dir whose name ends with suffix and returns the
// decoded objects. kind names the record type ("job", "document", ...) in logs and errors.
// A missing directory is not an error: it yields an empty, non-nil slice so the import
// payload carries [] instead of null. Unreadable or malformed files are skipped with a warning.
func readDigitizeFiles(dir, suffix, kind string) ([]interface{}, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Infof("No %s directory found, skipping %s import\n", filepath.Base(dir), kind)

		return []interface{}{}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s files: %w", kind, err)
	}

	records := make([]interface{}, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			logger.Warningf("Failed to read %s file %s: %v\n", kind, entry.Name(), err)

			continue
		}

		var record map[string]interface{}
		if err := json.Unmarshal(data, &record); err != nil {
			logger.Warningf("Failed to parse %s file %s: %v\n", kind, entry.Name(), err)

			continue
		}

		records = append(records, record)
	}

	logger.Infof("Read %d %s(s) from cache\n", len(records), kind)

	return records, nil
}

// DigitizeRestoreClient wraps the HTTP client for digitize restore operations.
type DigitizeRestoreClient struct {
	client *resty.Client
}

// NewDigitizeRestoreClient creates a new digitize restore client.
func NewDigitizeRestoreClient(serviceURL string) *DigitizeRestoreClient {
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

	return &DigitizeRestoreClient{
		client: client,
	}
}

// CallImportAPI calls the digitize service Import API with the metadata payload.
func (c *DigitizeRestoreClient) CallImportAPI(ctx context.Context, payload map[string]interface{}) error {
	logger.Infoln("Calling digitize Import API...")

	// Prepare response container
	var importResponse map[string]interface{}

	// Make the API call using the reusable HTTP client
	logger.Infoln("Sending import request to: /v1/import")
	resp, err := c.client.R().
		SetContext(ctx).
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

// CallImportContentAPI uploads one digitized output file to the digitize service.
// It returns true when the file was written and false when it already existed.
func (c *DigitizeRestoreClient) CallImportContentAPI(ctx context.Context, file digitizedContentFile) (bool, error) {
	raw, err := os.ReadFile(file.path)
	if err != nil {
		return false, fmt.Errorf("failed to read content file %s: %w", file.path, err)
	}

	result := json.RawMessage(raw)

	if file.format != "json" {
		// md/txt content is sent as a JSON string.
		if result, err = json.Marshal(string(raw)); err != nil {
			return false, fmt.Errorf("failed to encode content of %s: %w", file.docID, err)
		}
	}

	var response map[string]interface{}

	resp, err := c.client.R().
		SetContext(ctx).
		SetPathParam("doc_id", file.docID).
		SetBody(map[string]interface{}{"result": result, "output_format": file.format}).
		SetResult(&response).
		Post("/v1/import/content/{doc_id}")
	if err != nil {
		return false, fmt.Errorf("failed to call import content API for %s: %w", file.docID, err)
	}

	if resp.IsError() {
		return false, fmt.Errorf("import content API for %s returned HTTP %d: %s", file.docID, resp.StatusCode(), resp.String())
	}

	return response["status"] == "imported", nil
}

func logImportSummary(importResponse map[string]interface{}) {
	summary, ok := importResponse["summary"].(map[string]interface{})
	if !ok {
		return
	}

	logger.Infoln("Import summary:")

	if jobs, ok := summary["jobs"].(map[string]interface{}); ok {
		logger.Infof("  Jobs - imported: %d, skipped: %d, failed: %d\n",
			utils.GetNumericValFromMap(jobs, "imported"), utils.GetNumericValFromMap(jobs, "skipped"), utils.GetNumericValFromMap(jobs, "failed"))
	}

	if docs, ok := summary["documents"].(map[string]interface{}); ok {
		logger.Infof("  Documents - imported: %d, skipped: %d, failed: %d\n",
			utils.GetNumericValFromMap(docs, "imported"), utils.GetNumericValFromMap(docs, "skipped"), utils.GetNumericValFromMap(docs, "failed"))
	}

	if checksums, ok := summary["document_checksums"].(map[string]interface{}); ok && utils.GetNumericValFromMap(checksums, "total_received") > 0 {
		logger.Infof("  Document checksums - imported: %d, skipped: %d, failed: %d\n",
			utils.GetNumericValFromMap(checksums, "imported"), utils.GetNumericValFromMap(checksums, "skipped"), utils.GetNumericValFromMap(checksums, "failed"))
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

	logger.Infof("Import completed with %d warning(s)\n", len(warnings))
}

// RestoreDigitize restores digitize metadata (jobs, documents, checksums) from backupFile
// through the Import API and, when the backup contains digitized output files, uploads
// them to the digitize service as well.
func RestoreDigitize(ctx context.Context, digitizeURL, backupFile string) error {
	backupDir, cleanup, err := podmanrestore.ExtractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	importPayload, err := constructMetadataFromCache(backupDir)
	if err != nil {
		return err
	}

	contentFiles, err := listDigitizedContentFiles(backupDir)
	if err != nil {
		return err
	}

	client := NewDigitizeRestoreClient(digitizeURL)
	if err := client.CallImportAPI(ctx, importPayload); err != nil {
		return err
	}

	return restoreDigitizedContent(ctx, client, contentFiles)
}

// restoreDigitizedContent uploads digitized output files; documents must already be imported.
func restoreDigitizedContent(ctx context.Context, client *DigitizeRestoreClient, files []digitizedContentFile) error {
	if len(files) == 0 {
		return nil
	}

	logger.Infof("Restoring %d digitized content file(s)\n", len(files))

	var imported, skipped int

	for _, file := range files {
		written, err := client.CallImportContentAPI(ctx, file)
		if err != nil {
			return err
		}

		if written {
			imported++
		} else {
			skipped++
		}
	}

	logger.Infof("Digitized content - imported: %d, skipped: %d\n", imported, skipped)

	return nil
}

// GetDigitizeAPIURL extracts the digitize API URL from application details.
// It searches the application's services for one with CatalogID == "digitize"
// and returns the URL of the first endpoint with type == "api".
func GetDigitizeAPIURL(appDetails *catalogTypes.Application) (string, error) {
	for _, service := range appDetails.Services {
		if service.CatalogID == "digitize" {
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
