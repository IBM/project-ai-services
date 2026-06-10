package restore

import (
	"context"
	"fmt"
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

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	retryBackoffMultiplier = 2
	secretKeyValueParts    = 2
)

// RestoreOpenSearch restores OpenSearch data using podman sidecar approach.
func RestoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	logger.Infof("Restoring OpenSearch data for template: %s\n", templateID, 0)
	logger.Infof("OpenSearch Import (Sidecar Container Approach)\n", 0)

	// Find OpenSearch container and get pod ID
	containerName, podID, err := findContainerAndPod(ctx, templateID)
	if err != nil {
		return err
	}

	logger.Infof("Container: %s\n", containerName, 0)
	logger.Infof("Pod ID: %s\n", podID, 0)

	// Extract and locate backup directory
	backupDir, cleanup, err := ExtractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Manage sidecar lifecycle and perform restore
	return manageSidecarWithGo(ctx, podID, backupDir)
}

// findContainerAndPod finds the OpenSearch container and its pod ID.
func findContainerAndPod(ctx context.Context, templateID string) (string, string, error) {
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
func manageSidecarWithGo(ctx context.Context, podID, backupDir string) error {
	sidecarName := fmt.Sprintf("opensearch-restore-sidecar-%d", time.Now().Unix())

	// Create and start sidecar container
	containerID, err := createAndStartSidecar(ctx, sidecarName, podID)
	if err != nil {
		return fmt.Errorf("failed to create and start sidecar: %w", err)
	}

	// Ensure cleanup happens
	defer func() {
		logger.Infof("Cleaning up sidecar container...\n", 0)
		stopErr := containers.Stop(ctx, containerID, nil)
		if stopErr != nil {
			logger.Warningf("Failed to stop sidecar container %s: %v\n", containerID, stopErr)
		}
		// Note: Container has Remove=true, so it will be auto-removed when stopped
		// No need to explicitly remove it
		logger.Infof("Sidecar container cleanup completed\n", 0)
	}()

	// Prepare sidecar and perform restore
	return prepareSidecarAndRestore(ctx, containerID, backupDir)
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
			Image: vars.ToolImage,
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
