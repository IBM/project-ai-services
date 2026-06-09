package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/bindings/secrets"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/httpclient"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimePodman "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	retryBackoffMultiplier = 2
	secretKeyValueParts    = 2
	importTimeoutMinutes   = 5
)

// Restore restores application data from a backup file for Podman runtime.
func (p *PodmanApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name, 0)
	logger.Infof("Target: %s\n", opts.Target, 0)
	logger.Infof("Backup file: %s\n", opts.BackupFile, 0)

	// Get application details from catalog API
	appDetails, err := getApplicationDetails(opts.Name)
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
		componentID, err := getComponentID(appDetails, opts.Target)
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
	logger.Infof("Restoring OpenSearch data for template: %s\n", templateID, 0)
	logger.Infof("OpenSearch Import (Sidecar Container Approach)\n", 0)

	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Find OpenSearch container and get pod ID
	containerName, podID, err := p.findContainerAndPod(podmanCtx, templateID)
	if err != nil {
		return err
	}

	logger.Infof("Container: %s\n", containerName, 0)
	logger.Infof("Pod ID: %s\n", podID, 0)

	// Extract and locate backup directory
	backupDir, cleanup, err := extractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Manage sidecar lifecycle and perform restore
	return p.manageSidecarWithGo(ctx, podID, backupDir)
}

// getPodmanContext extracts the Podman context from the runtime client.
func (p *PodmanApplication) getPodmanContext() (context.Context, error) {
	podmanClient, ok := p.runtime.(*runtimePodman.PodmanClient)
	if !ok {
		return nil, fmt.Errorf("runtime is not a Podman client")
	}

	return podmanClient.Context, nil
}

// findContainerAndPod finds the OpenSearch container and its pod ID.
func (p *PodmanApplication) findContainerAndPod(ctx context.Context, templateID string) (string, string, error) {
	containerName, err := findOpenSearchContainer(ctx, templateID)
	if err != nil {
		return "", "", fmt.Errorf("failed to find OpenSearch container: %w", err)
	}

	podID, err := getPodID(ctx, containerName)
	if err != nil {
		return "", "", fmt.Errorf("failed to get pod ID: %w", err)
	}

	return containerName, podID, nil
}

// extractAndLocateBackup extracts the backup archive and locates the backup directory.
func extractAndLocateBackup(backupFile string) (string, func(), error) {
	logger.Infof("Extracting backup archive...\n", 0)

	tempDir, err := os.MkdirTemp("", "opensearch-restore-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Errorf("Failed to cleanup temp directory %s: %v\n", tempDir, err)
		}
	}

	if err := utils.ExtractTarGz(backupFile, tempDir); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("failed to extract backup: %w", err)
	}

	backupDir, err := locateBackupDirectory(tempDir)
	if err != nil {
		cleanup()

		return "", nil, err
	}

	return backupDir, cleanup, nil
}

// locateBackupDirectory determines the backup directory path supporting both formats.
func locateBackupDirectory(tempDir string) (string, error) {
	backupDirOld := filepath.Join(tempDir, "backup")
	backupDirNew := filepath.Join(tempDir, "opensearch_backup")

	if _, err := os.Stat(backupDirOld); err == nil {
		return backupDirOld, nil
	}

	if _, err := os.Stat(backupDirNew); err == nil {
		return backupDirNew, nil
	}

	return "", formatBackupNotFoundError(tempDir)
}

// formatBackupNotFoundError creates a detailed error message for missing backup directory.
func formatBackupNotFoundError(tempDir string) error {
	entries, listErr := os.ReadDir(tempDir)
	if listErr != nil {
		return fmt.Errorf("backup directory not found in archive")
	}

	var extractedDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			extractedDirs = append(extractedDirs, entry.Name())
		}
	}

	if len(extractedDirs) > 0 {
		return fmt.Errorf("backup directory not found in archive. Expected 'backup/' or 'opensearch_backup/' but found: %v", extractedDirs)
	}

	return fmt.Errorf("backup directory not found in archive")
}

// getApplicationDetails retrieves the application details from the catalog API.
// First lists applications to find the app ID by name, then gets full details by ID.
func getApplicationDetails(appName string) (*catalogTypes.Application, error) {
	catalogClient, httpClient, err := setupCatalogClients()
	if err != nil {
		return nil, err
	}

	appID, err := findApplicationID(httpClient, catalogClient, appName)
	if err != nil {
		return nil, err
	}

	return fetchApplicationDetails(httpClient, catalogClient, appID)
}

// setupCatalogClients creates and returns catalog API and HTTP clients.
func setupCatalogClients() (*client.Client, *httpclient.HTTPClient, error) {
	catalogClient, err := client.New()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create catalog API client: %w", err)
	}

	httpClient := httpclient.New(catalogClient.ServerURL())

	return catalogClient, httpClient, nil
}

// findApplicationID searches for an application by name across all pages.
func findApplicationID(httpClient *httpclient.HTTPClient, catalogClient *client.Client, appName string) (string, error) {
	listResponse, err := fetchApplicationList(httpClient, catalogClient, 1)
	if err != nil {
		return "", err
	}

	appID := searchApplicationInList(listResponse.Data, appName)
	if appID != "" {
		return appID, nil
	}

	if !listResponse.Pagination.HasNext {
		return "", fmt.Errorf("application with name '%s' not found", appName)
	}

	return searchRemainingPages(httpClient, catalogClient, appName, listResponse.Pagination.TotalPages)
}

// fetchApplicationList retrieves a page of applications from the catalog API.
func fetchApplicationList(httpClient *httpclient.HTTPClient, catalogClient *client.Client, page int) (*catalogTypes.ApplicationListResponse, error) {
	var listResponse catalogTypes.ApplicationListResponse

	query := map[string]string{"page_size": "100"}
	if page > 1 {
		query["page"] = fmt.Sprintf("%d", page)
	}

	err := httpClient.Do(httpclient.Request{
		Method:   http.MethodGet,
		Endpoint: "/api/v1/applications",
		Headers:  map[string]string{"Authorization": "Bearer " + catalogClient.AccessToken()},
		Query:    query,
		Out:      &listResponse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list applications from catalog API: %w", err)
	}

	return &listResponse, nil
}

// searchApplicationInList searches for an application by name in a list.
func searchApplicationInList(apps []catalogTypes.Application, appName string) string {
	for _, app := range apps {
		if app.Name == appName {
			return app.ID
		}
	}

	return ""
}

// searchRemainingPages searches for an application across remaining pages.
func searchRemainingPages(httpClient *httpclient.HTTPClient, catalogClient *client.Client, appName string, totalPages int) (string, error) {
	for page := 2; page <= totalPages; page++ {
		listResponse, err := fetchApplicationList(httpClient, catalogClient, page)
		if err != nil {
			return "", err
		}

		appID := searchApplicationInList(listResponse.Data, appName)
		if appID != "" {
			return appID, nil
		}
	}

	return "", fmt.Errorf("application with name '%s' not found", appName)
}

// fetchApplicationDetails retrieves full application details by ID.
func fetchApplicationDetails(httpClient *httpclient.HTTPClient, catalogClient *client.Client, appID string) (*catalogTypes.Application, error) {
	var appDetails catalogTypes.Application

	err := httpClient.Do(httpclient.Request{
		Method:   http.MethodGet,
		Endpoint: fmt.Sprintf("/api/v1/applications/%s", appID),
		Headers:  map[string]string{"Authorization": "Bearer " + catalogClient.AccessToken()},
		Out:      &appDetails,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get application details from catalog API: %w", err)
	}

	return &appDetails, nil
}

// getComponentID extracts the component ID for the specified target from application details.
func getComponentID(appDetails *catalogTypes.Application, target string) (string, error) {
	// Map target names to component provider names
	targetToProvider := map[string]string{
		"opensearch": "opensearch",
	}

	providerName, ok := targetToProvider[target]
	if !ok {
		return "", fmt.Errorf("unknown target: %s", target)
	}

	// Search through services and their components
	for _, service := range appDetails.Services {
		for _, component := range service.Component {
			if component.Provider == providerName {
				return component.ID, nil
			}
		}
	}

	return "", fmt.Errorf("component with provider '%s' not found for target '%s'", providerName, target)
}

// findOpenSearchContainer finds the OpenSearch container for the given template ID using Podman SDK.
func findOpenSearchContainer(ctx context.Context, templateID string) (string, error) {
	// Parse templateID as UUID to ensure it's valid
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		return "", fmt.Errorf("invalid template ID format: %w", err)
	}

	// List containers with filters
	filters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/template=%s", templateUUID.String())},
		"name":  {"opensearch"},
	}

	listOpts := &containers.ListOptions{}
	listOpts.WithFilters(filters)

	containerList, err := containers.List(ctx, listOpts)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containerList) == 0 {
		return "", fmt.Errorf("OpenSearch container not found for template ID: %s", templateUUID.String())
	}

	// Return the first matching container name
	return containerList[0].Names[0], nil
}

// getPodID gets the pod ID for a container using Podman SDK.
func getPodID(ctx context.Context, containerName string) (string, error) {
	// Inspect the container to get pod information
	containerData, err := containers.Inspect(ctx, containerName, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	podID := containerData.Pod
	if podID == "" {
		return "", fmt.Errorf("container is not part of a pod. Sidecar approach requires pod deployment")
	}

	return podID, nil
}

// manageSidecarWithGo manages the lifecycle of a podman sidecar container.
func (p *PodmanApplication) manageSidecarWithGo(ctx context.Context, podID, backupDir string) error {
	sidecarName := fmt.Sprintf("opensearch-restore-sidecar-%d", time.Now().Unix())

	// Get the Podman context from the runtime client
	podmanClient, ok := p.runtime.(*runtimePodman.PodmanClient)
	if !ok {
		return fmt.Errorf("runtime is not a Podman client")
	}
	podmanCtx := podmanClient.Context

	// Create and start sidecar container
	containerID, err := createAndStartSidecar(podmanCtx, sidecarName, podID)
	if err != nil {
		return fmt.Errorf("failed to create and start sidecar: %w", err)
	}

	// Ensure cleanup happens
	defer func() {
		logger.Infof("Cleaning up sidecar container...\n", 0)
		stopErr := containers.Stop(podmanCtx, containerID, nil)
		if stopErr != nil {
			logger.Warningf("Failed to stop sidecar container %s: %v\n", containerID, stopErr)
		}
		// Note: Container has Remove=true, so it will be auto-removed when stopped
		// No need to explicitly remove it
		logger.Infof("Sidecar container cleanup completed\n", 0)
	}()

	// Prepare sidecar and perform restore
	return prepareSidecarAndRestore(podmanCtx, containerID, backupDir)
}

// createAndStartSidecar creates and starts a sidecar container.
func createAndStartSidecar(ctx context.Context, sidecarName, podID string) (string, error) {
	logger.Infof("Starting sidecar container...\n", 0)

	s := &specgen.SpecGenerator{
		ContainerBasicConfig: specgen.ContainerBasicConfig{
			Name:    sidecarName,
			Remove:  utils.BoolPtr(true), // Auto-remove container when stopped
			Command: []string{"sleep", "3600"},
			Pod:     podID,
		},
		ContainerStorageConfig: specgen.ContainerStorageConfig{
			Image: constants.SidecarImage,
		},
		ContainerHealthCheckConfig: specgen.ContainerHealthCheckConfig{
			// Set HealthConfig to nil to disable health checks
			HealthConfig: nil,
			// Set HealthLogDestination to /tmp to satisfy directory requirement
			// HealthLogDestination is a non-pointer string field that defaults to ""
			// Podman requires this to be a directory path when specified
			HealthLogDestination: "/tmp",
		},
	}

	createResponse, err := containers.CreateWithSpec(ctx, s, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create sidecar container: %w", err)
	}

	containerID := createResponse.ID
	if err := containers.Start(ctx, containerID, nil); err != nil {
		return "", fmt.Errorf("failed to start sidecar container: %w", err)
	}

	return containerID, nil
}

// prepareSidecarAndRestore prepares the sidecar container and performs the restore.
func prepareSidecarAndRestore(ctx context.Context, containerID, backupDir string) error {
	osPassword, err := getOpenSearchPasswordFromSecret(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	if err := installJQInSidecar(ctx, containerID); err != nil {
		return err
	}

	backupOSDir, containerBackupPath, err := determineBackupPaths(backupDir)
	if err != nil {
		return err
	}

	if err := copyBackupToSidecar(ctx, containerID, backupOSDir, containerBackupPath); err != nil {
		return err
	}

	if err := performRestoreWithCurl(ctx, containerID, "localhost:9200", osPassword, containerBackupPath); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	logger.Infof("OpenSearch import completed!\n", 0)

	return nil
}

// installJQInSidecar installs jq in the sidecar container with retry logic.
func installJQInSidecar(ctx context.Context, containerID string) error {
	logger.Infof("Installing jq in sidecar...\n", 0)

	installScript := `
# Create cache directory with proper permissions
mkdir -p /var/cache/yum/metadata 2>/dev/null || true
chmod -R 777 /var/cache/yum 2>/dev/null || true

# Clean and install (jq for JSON processing)
microdnf clean all 2>&1
microdnf install -y --nodocs --setopt=install_weak_deps=0 jq 2>&1
`

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			logger.Infof("Retrying installation (attempt %d/%d)...\n", i+1, maxRetries, 0)
			time.Sleep(time.Duration(i*retryBackoffMultiplier) * time.Second)
		}

		if err := execInContainer(ctx, containerID, []string{"sh", "-c", installScript}); err == nil {
			logger.Infof("Successfully installed jq\n", 0)

			return nil
		}

		logger.Warningf("Installation attempt %d failed\n", i+1)
	}

	return fmt.Errorf("failed to install jq after %d retries", maxRetries)
}

// determineBackupPaths determines the backup directory paths based on format.
func determineBackupPaths(backupDir string) (string, string, error) {
	const containerBackupPath = "/tmp/opensearch_backup"

	var backupOSDir string

	if filepath.Base(backupDir) == "opensearch_backup" {
		backupOSDir = backupDir
	} else {
		backupOSDir = filepath.Join(backupDir, "opensearch")
	}

	if _, err := os.Stat(backupOSDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("OpenSearch backup directory not found: %s", backupOSDir)
	}

	return backupOSDir, containerBackupPath, nil
}

// copyBackupToSidecar copies backup files to the sidecar container.
func copyBackupToSidecar(ctx context.Context, containerID, backupOSDir, containerBackupPath string) error {
	logger.Infof("Copying backup files to sidecar...\n", 0)

	if err := copyDirToContainer(ctx, containerID, backupOSDir, containerBackupPath); err != nil {
		return fmt.Errorf("failed to copy backup files: %w", err)
	}

	return nil
}

// execInContainer executes a command in a container using podman exec command.
// Note: Using exec.Command instead of SDK because the SDK's exec API is complex
// and requires handlers.ExecCreateConfig which is not easily accessible.
func execInContainer(ctx context.Context, containerID string, cmd []string) error {
	// Build podman exec command
	args := []string{"exec", containerID}
	args = append(args, cmd...)

	execCmd := exec.CommandContext(ctx, "podman", args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// copyDirToContainer copies a directory to a container using podman cp command.
// Note: Using exec.Command instead of SDK because the SDK's copy API requires
// tar archive handling which is complex.
func copyDirToContainer(ctx context.Context, containerID, srcDir, destDir string) error {
	// Verify source directory exists
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("source directory does not exist: %s", srcDir)
	}

	// Use podman cp command to copy directory
	// Format: podman cp <src>/. <container>:<dest>
	// The "/." ensures we copy the contents of the directory, not the directory itself
	cpCmd := exec.CommandContext(ctx, "podman", "cp", srcDir+"/.", fmt.Sprintf("%s:%s", containerID, destDir))
	output, err := cpCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to copy directory: %w, output: %s", err, string(output))
	}

	return nil
}

// performRestoreWithCurl performs the OpenSearch restore using curl commands in container.
func performRestoreWithCurl(ctx context.Context, containerID, osHost, osPassword, backupDir string) error {
	// Verify backup directory exists in container
	verifyScript := fmt.Sprintf("test -d %s && echo 'exists' || echo 'not found'", backupDir)
	if err := execInContainer(ctx, containerID, []string{"sh", "-c", verifyScript}); err != nil {
		return fmt.Errorf("backup directory not found in container: %w", err)
	}

	// List indices using podman exec with output capture
	listScript := fmt.Sprintf("cd %s && ls *_data.json 2>/dev/null | sed 's/_data.json//' || true", backupDir)
	listCmd := exec.CommandContext(ctx, "podman", "exec", containerID, "sh", "-c", listScript)
	output, err := listCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list indices: %w, output: %s", err, string(output))
	}

	// Parse indices from output
	indicesStr := strings.TrimSpace(string(output))
	if indicesStr == "" {
		return fmt.Errorf("no indices found in backup directory")
	}

	indices := strings.Split(indicesStr, "\n")
	logger.Infof("Found %d indices to restore\n", len(indices), 0)

	// Restore each index
	restoredCount := 0
	var lastErr error

	for _, indexName := range indices {
		indexName = strings.TrimSpace(indexName)
		if indexName == "" {
			continue
		}

		if err := restoreIndexWithCurl(ctx, containerID, osHost, osPassword, backupDir, indexName); err != nil {
			logger.Errorf("Failed to restore index %s: %v\n", indexName, err)
			lastErr = err

			continue
		}

		restoredCount++
	}

	if restoredCount == 0 && lastErr != nil {
		return fmt.Errorf("failed to restore any indices, last error: %w", lastErr)
	}

	if lastErr != nil {
		logger.Warningf("Restore completed with errors. Successfully restored %d/%d indices\n", restoredCount, len(indices))
	} else {
		logger.Infof("✓ Restore completed successfully. Restored %d indices\n", restoredCount, 0)
	}

	return nil
}

// restoreIndexWithCurl restores a single index using curl in container.
func restoreIndexWithCurl(ctx context.Context, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Restoring index: %s\n", indexName, 0)

	curlBase := fmt.Sprintf("curl -k -u admin:%s https://%s", osPassword, osHost)

	// Verify required backup files exist
	requiredFiles := []string{
		fmt.Sprintf("%s/%s_mapping.json", backupDir, indexName),
		fmt.Sprintf("%s/%s_settings.json", backupDir, indexName),
		fmt.Sprintf("%s/%s_data.json", backupDir, indexName),
	}

	for _, file := range requiredFiles {
		verifyScript := fmt.Sprintf("test -f %s && echo 'exists' || echo 'not found'", file)
		if err := execInContainer(ctx, containerID, []string{"sh", "-c", verifyScript}); err != nil {
			return fmt.Errorf("required backup file not found: %s", file)
		}
	}

	// Delete existing index if it exists
	deleteCmd := []string{"sh", "-c", fmt.Sprintf("%s/%s -X DELETE -s -o /dev/null 2>/dev/null || true", curlBase, indexName)}
	_ = execInContainer(ctx, containerID, deleteCmd) // Ignore error if index doesn't exist

	// Create index with settings and mappings
	createScript := fmt.Sprintf(`
MAPPING=$(cat %s/%s_mapping.json | jq -c '."%s".mappings')
SETTINGS=$(cat %s/%s_settings.json | jq -c '."%s".settings.index | del(.creation_date, .uuid, .version, .provided_name)')
BODY=$(jq -n --argjson settings "{\"index\": $SETTINGS}" --argjson mappings "$MAPPING" '{settings: $settings, mappings: $mappings}')
RESPONSE=$(%s/%s -X PUT -H "Content-Type: application/json" -d "$BODY" -s -w "%%{http_code}")
HTTP_CODE=$(echo "$RESPONSE" | tail -c 4)
if [ "$HTTP_CODE" != "200" ]; then
	 echo "Failed to create index. HTTP code: $HTTP_CODE"
	 exit 1
fi
`, backupDir, indexName, indexName, backupDir, indexName, indexName, curlBase, indexName)

	createCmd := []string{"sh", "-c", createScript}
	if err := execInContainer(ctx, containerID, createCmd); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	// Bulk index documents
	bulkScript := fmt.Sprintf(`
RESPONSE=$(cat %s/%s_data.json | jq -c '.[] | {"index": {"_index": "%s", "_id": ._id}}, ._source' | \
%s/_bulk -X POST -H "Content-Type: application/x-ndjson" --data-binary @- -s -w "%%{http_code}")
HTTP_CODE=$(echo "$RESPONSE" | tail -c 4)
if [ "$HTTP_CODE" != "200" ]; then
	 echo "Failed to bulk index documents. HTTP code: $HTTP_CODE"
	 exit 1
fi
`, backupDir, indexName, indexName, curlBase)

	bulkCmd := []string{"sh", "-c", bulkScript}
	if err := execInContainer(ctx, containerID, bulkCmd); err != nil {
		return fmt.Errorf("failed to bulk index documents: %w", err)
	}

	// Refresh index
	refreshCmd := []string{"sh", "-c", fmt.Sprintf("%s/%s/_refresh -X POST -s -o /dev/null", curlBase, indexName)}
	if err := execInContainer(ctx, containerID, refreshCmd); err != nil {
		return fmt.Errorf("failed to refresh index: %w", err)
	}

	logger.Infof("    ✓ Index restored\n", 0)

	return nil
}

// getOpenSearchPasswordFromSecret retrieves the OpenSearch password from the Podman secret using SDK.
func getOpenSearchPasswordFromSecret(ctx context.Context, containerID string) (string, error) {
	secretName, err := getSecretNameFromContainer(ctx, containerID)
	if err != nil {
		return "", err
	}

	logger.Infof("Reading password from secret: %s\n", secretName, 0)

	secretData, err := fetchSecretData(ctx, secretName)
	if err != nil {
		return "", err
	}

	password, err := extractPasswordFromSecretData(secretData)
	if err != nil {
		return "", err
	}

	logger.Infof("Successfully retrieved password from secret\n", 0)

	return password, nil
}

// getSecretNameFromContainer retrieves the secret name from the container's pod labels.
func getSecretNameFromContainer(ctx context.Context, containerID string) (string, error) {
	containerData, err := containers.Inspect(ctx, containerID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	podID := containerData.Pod
	if podID == "" {
		return "", fmt.Errorf("container is not part of a pod")
	}

	podData, err := pods.Inspect(ctx, podID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect pod: %w", err)
	}

	secretName, ok := podData.Labels["ai-services.io/secret"]
	if !ok || secretName == "" {
		return "", fmt.Errorf("secret label 'ai-services.io/secret' not found in pod labels")
	}

	return secretName, nil
}

// fetchSecretData retrieves the secret data from Podman.
func fetchSecretData(ctx context.Context, secretName string) (string, error) {
	inspectOpts := &secrets.InspectOptions{}
	inspectOpts.WithShowSecret(true)

	secretInfo, err := secrets.Inspect(ctx, secretName, inspectOpts)
	if err != nil {
		return "", fmt.Errorf("failed to inspect secret %s: %w", secretName, err)
	}

	if secretInfo.SecretData == "" {
		return "", fmt.Errorf("secret data is empty for secret %s", secretName)
	}

	return secretInfo.SecretData, nil
}

// extractPasswordFromSecretData parses secret data to extract the password field.
func extractPasswordFromSecretData(secretData string) (string, error) {
	lines := strings.Split(secretData, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", secretKeyValueParts)
		if len(parts) != secretKeyValueParts {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "password" && value != "" {
			return value, nil
		}
	}

	return "", fmt.Errorf("password field not found in secret data")
}

// Made with Bob

// restoreDigitize restores digitize metadata using the Import API.
func (p *PodmanApplication) restoreDigitize(ctx context.Context, appDetails *catalogTypes.Application, backupFile string) error {
	logger.Infof("Restoring digitize metadata\n", 0)
	logger.Infof("Digitize Import (API-based Approach)\n", 0)

	// Extract and locate backup directory
	backupDir, cleanup, err := extractAndLocateBackup(backupFile)
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
