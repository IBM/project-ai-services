package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/httpclient"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimePodman "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Restore restores application data from a backup file for Podman runtime.
func (p *PodmanApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name, 0)
	logger.Infof("Target: %s\n", opts.Target, 0)
	logger.Infof("Backup file: %s\n", opts.BackupFile, 0)

	// Get application details from catalog API using existing utility
	appDetails, err := cliUtils.GetAppDetailsWithComponents(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID, 0)

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		// Get component ID for opensearch
		componentID, err := cliUtils.GetComponentID(appDetails, opts.Target)
		if err != nil {
			return fmt.Errorf("failed to get component ID: %w", err)
		}
		logger.Infof("Component ID: %s\n", componentID, 0)

		return p.restoreOpenSearch(ctx, componentID, absFilename)
	case "digitize":
		return p.restoreDigitize(ctx, appDetails, absFilename)
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}

// restoreOpenSearch restores OpenSearch data using podman sidecar approach.
func (p *PodmanApplication) restoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Call the OpenSearch-specific restore function
	return restore.RestoreOpenSearch(podmanCtx, templateID, backupFile)
}

// getPodmanContext extracts the Podman context from the runtime client.
func (p *PodmanApplication) getPodmanContext() (context.Context, error) {
	podmanClient, ok := p.runtime.(*runtimePodman.PodmanClient)
	if !ok {
		return nil, fmt.Errorf("runtime is not a Podman client")
	}

	return podmanClient.Context, nil
}

// restoreDigitize restores digitize metadata using the Import API.
func (p *PodmanApplication) restoreDigitize(ctx context.Context, appDetails *catalogTypes.Application, backupFile string) error {
	logger.Infof("Restoring digitize metadata\n", 0)
	logger.Infof("Digitize Import (API-based Approach)\n", 0)

	// Extract and locate backup directory
	backupDir, cleanup, err := restore.ExtractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Construct metadata from cache files
	importPayload, err := constructMetadataFromCache(backupDir)
	if err != nil {
		return err
	}

	// Get digitize service API URL from application details
	digitizeURL, err := getDigitizeAPIURL(appDetails)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL, 0)

	// Call Import API
	if err := callDigitizeImportAPI(digitizeURL, importPayload); err != nil {
		return err
	}

	logger.Infof("✓ Digitize metadata restore completed successfully\n", 0)

	return nil
}

// constructMetadataFromCache reads cache files and constructs the Import API payload.
func constructMetadataFromCache(backupDir string) (map[string]interface{}, error) {
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
			logger.Warningf("Failed to read job file %s: %v\n", entry.Name(), err, 0)

			continue
		}

		var job map[string]interface{}
		if err := json.Unmarshal(data, &job); err != nil {
			logger.Warningf("Failed to parse job file %s: %v\n", entry.Name(), err, 0)

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
			logger.Warningf("Failed to read document file %s: %v\n", entry.Name(), err, 0)

			continue
		}

		var doc map[string]interface{}
		if err := json.Unmarshal(data, &doc); err != nil {
			logger.Warningf("Failed to parse document file %s: %v\n", entry.Name(), err, 0)

			continue
		}

		documents = append(documents, doc)
	}

	logger.Infof("Read %d document(s) from cache\n", len(documents), 0)

	return documents, nil
}

// getDigitizeAPIURL extracts the digitize API URL from application details.
func getDigitizeAPIURL(appDetails *catalogTypes.Application) (string, error) {
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

// callDigitizeImportAPI calls the digitize service Import API with the metadata payload.
func callDigitizeImportAPI(serviceURL string, payload map[string]interface{}) error {
	logger.Infof("Calling digitize Import API...\n", 0)

	// Create HTTP client
	client := httpclient.New(serviceURL)

	// Prepare response container
	var importResponse map[string]interface{}

	// Make the API call using the reusable HTTP client
	logger.Infof("Sending import request to: %s/v1/import\n", serviceURL, 0)
	err := client.Do(httpclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/v1/import",
		Payload:  payload,
		Out:      &importResponse,
	})

	if err != nil {
		return fmt.Errorf("failed to call import API: %w", err)
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
