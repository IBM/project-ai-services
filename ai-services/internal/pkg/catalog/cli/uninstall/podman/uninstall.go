package podman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// UninstallCatalog removes the catalog service and all associated resources.
func UninstallCatalog(ctx context.Context, autoYes, skipCleanup bool) error {
	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	pods, err := validateCatalogExists(rt)
	if err != nil || len(pods) == 0 {
		return err
	}

	// Warn about potential application staleness
	logger.Warningln("Ensure no applications are running before uninstalling the catalog, as they may go stale when the catalog is uninstalled and will need to be deleted manually")

	// Confirm deletion if not auto-yes
	if !autoYes {
		if confirmed, err := confirmDeletion(pods); !confirmed || err != nil {
			return err
		}
	}

	return performCleanup(rt, pods, skipCleanup)
}

// validateCatalogExists checks if catalog resources exist and returns them.
func validateCatalogExists(rt *podman.PodmanClient) ([]types.Pod, error) {
	// Check if catalog pods exist
	pods, err := rt.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", catalogConstants.CatalogAppName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infoln("Catalog service is not deployed")

		return nil, nil
	}

	logger.Infof("Found %d catalog pod(s)\n", len(pods))

	return pods, nil
}

// confirmDeletion prompts the user to confirm deletion and logs pods to be deleted.
func confirmDeletion(pods []types.Pod) (bool, error) {
	// Print pods to be deleted
	logger.Infoln("Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	// Confirm deletion
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.Infoln("Deletion cancelled")

		return false, nil
	}

	return true, nil
}

// performCleanup executes all cleanup operations.
func performCleanup(rt *podman.PodmanClient, pods []types.Pod, skipCleanup bool) error {
	logger.Infoln("Proceeding with deletion...")

	// Retrieve the BaseDir from the catalog pod configuration
	var baseDir string
	config, _, err := catalogUtils.GetCatalogPodConfig(rt)
	if err != nil {
		logger.Warningf("Failed to retrieve BaseDir from catalog pod: %v. Using default BaseDir.\n", err)
		baseDir = utils.GetBaseDir()
	} else {
		baseDir = config.BaseDir
	}
	logger.Infof("Using base directory for cleanup: %s\n", baseDir)

	secretsToDelete, secretsToSkip := fetchSecretsToDelete(pods)
	secretsToDelete = append(secretsToDelete, catalogConstants.PodmanAuthSecret)

	volumesToDelete, volumesToSkip := fetchVolumesToDelete(pods)

	// Delete catalog pods
	if err := podsDeletion(rt, pods); err != nil {
		return err
	}

	// Delete catalog secrets
	if err := deleteSecrets(rt, secretsToDelete); err != nil {
		return err
	}

	// Delete volumes (only those without skip-cleanup label)
	if err := deleteVolumes(rt, volumesToDelete); err != nil {
		return err
	}

	// Delete caddy data
	caddyDataPath := filepath.Join(baseDir, "common")
	if err := dataDeletion(caddyDataPath); err != nil {
		return err
	}

	// Delete models data
	modelsDataPath := filepath.Join(baseDir, "models")
	if err := dataDeletion(modelsDataPath); err != nil {
		return err
	}

	// Delete database data and secrets
	if err := cleanupDatabaseResources(rt, secretsToSkip, volumesToSkip, skipCleanup); err != nil {
		return err
	}

	logger.Infoln("Catalog service removed successfully")

	return nil
}

// deleteSecrets removes the specified secrets.
func deleteSecrets(rt *podman.PodmanClient, secrets []string) error {
	for _, secret := range secrets {
		if err := rt.DeleteSecret(secret); err != nil {
			return err
		}
	}

	return nil
}

// cleanupDatabaseResources handles database volume and secret cleanup.
func cleanupDatabaseResources(rt *podman.PodmanClient, secretsToSkip []string, volumesToSkip []string, skipCleanup bool) error {
	if skipCleanup {
		logger.Infoln("Skipping database data cleanup (--skip-cleanup flag set)")

		return nil
	}

	// Delete catalog secrets
	if err := deleteSecrets(rt, secretsToSkip); err != nil {
		return err
	}

	// Delete volumes with skip-cleanup label (only when --skip-cleanup is not set)
	return deleteVolumes(rt, volumesToSkip)
}

// podsDeletion removes all catalog pods.
func podsDeletion(rt *podman.PodmanClient, pods []types.Pod) error {
	var errors []string

	for _, pod := range pods {
		logger.Infof("Deleting pod: %s\n", pod.Name)

		if err := rt.DeletePod(pod.ID, utils.BoolPtr(true)); err != nil {
			errors = append(errors, fmt.Sprintf("pod %s: %v", pod.Name, err))

			continue
		}

		logger.Infof("Successfully removed pod: %s\n", pod.Name)
	}

	// Aggregate errors at the end
	if len(errors) > 0 {
		return fmt.Errorf("failed to remove pods: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// We are currently associating secret names with pods via pod labels and relying on those labels for secret cleanup.
// Since this is not an ideal approach for managing secret deletion, we should design a more robust and reliable mechanism in the future.
// fetchSecretsToDelete fetches the secrets to delete and secrets which are to be deleted when --skip-cleanup is not set.
func fetchSecretsToDelete(pods []types.Pod) ([]string, []string) {
	var secretsToDelete, secretsToSkip []string
	for _, pod := range pods {
		// fetch secret name from pod labels
		if secretName, ok := pod.Labels[catalogConstants.CatalogSecretLabel]; ok {
			// check if it has skip-cleanup label
			if _, ok := pod.Labels[catalogConstants.CatalogSecretSkipLabel]; ok {
				secretsToSkip = append(secretsToSkip, secretName)
			} else {
				secretsToDelete = append(secretsToDelete, secretName)
			}
		}
	}

	return secretsToDelete, secretsToSkip
}

// fetchVolumesToDelete extracts volume names from pod labels and separates them based on skip-cleanup label.
// Returns two lists: volumes to delete immediately, and volumes to skip (only deleted when --skip-cleanup is not set).
func fetchVolumesToDelete(pods []types.Pod) ([]string, []string) {
	volumeMapToDelete := make(map[string]bool) // Use map to avoid duplicates
	volumeMapToSkip := make(map[string]bool)

	for _, pod := range pods {
		// fetch volume names from pod labels
		if volumeNames, ok := pod.Labels[catalogConstants.CatalogVolumeLabel]; ok && volumeNames != "" {
			// Check if this pod has skip-cleanup label for volumes
			_, hasSkipLabel := pod.Labels[catalogConstants.CatalogVolumeSkipLabel]

			// Split comma-separated volume names (in case a pod has multiple volumes)
			volumes := strings.Split(volumeNames, ",")
			for _, volumeName := range volumes {
				volumeName = strings.TrimSpace(volumeName)
				if volumeName != "" {
					if hasSkipLabel {
						volumeMapToSkip[volumeName] = true
					} else {
						volumeMapToDelete[volumeName] = true
					}
				}
			}
		}
	}

	// Convert maps to slices
	volumesToDelete := make([]string, 0, len(volumeMapToDelete))
	for volumeName := range volumeMapToDelete {
		volumesToDelete = append(volumesToDelete, volumeName)
	}

	volumesToSkip := make([]string, 0, len(volumeMapToSkip))
	for volumeName := range volumeMapToSkip {
		volumesToSkip = append(volumesToSkip, volumeName)
	}

	return volumesToDelete, volumesToSkip
}

// dataDeletion removes the specified data directory.
func dataDeletion(dataPath string) error {
	// Check if data directory exists
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		logger.Infof("data directory does not exist: %s\n", dataPath)

		return nil
	}

	logger.Infof("Deleting data at: %s\n", dataPath)

	// Remove the data directory
	if err := os.RemoveAll(dataPath); err != nil {
		return fmt.Errorf("failed to remove database data directory: %w", err)
	}

	logger.Infof("Successfully removed data at: %s\n", dataPath)

	return nil
}

// deleteVolumes removes the specified volumes.
func deleteVolumes(rt *podman.PodmanClient, volumeNames []string) error {
	if len(volumeNames) == 0 {
		// Just return if there are no volumes to delete.
		return nil
	}

	logger.Infof("Deleting %d volume(s)\n", len(volumeNames))

	var errors []string
	for _, volumeName := range volumeNames {
		logger.Infof("Deleting volume: %s\n", volumeName)

		if err := rt.DeleteVolume(volumeName); err != nil {
			// Ignore "not found" errors - volume already deleted or never existed
			if catalogUtils.IsNotFoundError(err) {
				logger.Infof("Volume %s already deleted or does not exist\n", volumeName)

				continue
			}

			errors = append(errors, fmt.Sprintf("volume %s: %v", volumeName, err))

			continue
		}

		logger.Infof("Successfully deleted volume: %s\n", volumeName)
	}

	// Aggregate errors at the end
	if len(errors) > 0 {
		return fmt.Errorf("failed to remove volumes: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// Made with Bob
