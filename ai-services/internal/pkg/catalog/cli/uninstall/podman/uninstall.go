package podman

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	clicommon "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/uninstall/utils"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// UninstallCatalog removes the catalog service and all associated resources.
func UninstallCatalog(ctx context.Context, opts cliutils.UninstallOptions) error {
	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	pods, err := clicommon.GetCatalogPods(ctx, rt)
	if err != nil || len(pods) == 0 {
		return err
	}

	// Warn about potential application staleness
	logger.Warningln("Ensure no applications are running before uninstalling the catalog, as they may go stale when the catalog is uninstalled and will need to be deleted manually")

	// Confirm deletion if not auto-yes
	if confirmed, err := podmanutils.ConfirmUninstall(ctx, pods, opts.AutoYes); !confirmed || err != nil {
		return err
	}

	return performCleanup(ctx, rt, pods, opts.SkipCleanup)
}

// performCleanup executes all cleanup operations.
func performCleanup(ctx context.Context, rt *podman.PodmanClient, pods []types.Pod, skipCleanup bool) error {
	logger.Infoln("Proceeding with deletion...")

	// Retrieve the BaseDir from the catalog pod configuration
	var baseDir string
	config, _, err := catalogUtils.GetCatalogPodConfig(ctx, rt)
	if err != nil {
		logger.Warningf("Failed to retrieve BaseDir from catalog pod: %v. Using default BaseDir.\n", err)
		baseDir = utils.GetBaseDir()
	} else {
		baseDir = config.BaseDir
	}
	logger.Infof("Using base directory for cleanup: %s\n", baseDir)

	secretsToDelete, secretsToSkip := fetchSecretsToDelete(pods)

	volumesToDelete, volumesToSkip := podmanutils.FetchVolumesToDelete(pods)

	// Delete catalog pods
	if err := podmanutils.DeletePods(ctx, rt, pods); err != nil {
		return err
	}

	// Delete catalog secrets
	if err := podmanutils.DeleteSecrets(ctx, rt, secretsToDelete); err != nil {
		return err
	}

	// Delete volumes (only those without skip-cleanup label)
	if err := podmanutils.DeleteVolumes(ctx, rt, volumesToDelete); err != nil {
		return err
	}

	// Delete models data
	modelsDataPath := filepath.Join(baseDir, "models")
	if err := podmanutils.RemoveDataDir(ctx, modelsDataPath); err != nil {
		return err
	}

	// Delete skip-cleanup resources (secrets and volumes preserved when --skip-cleanup is set)
	if err := podmanutils.CleanupSkippedResources(ctx, rt, secretsToSkip, volumesToSkip, skipCleanup); err != nil {
		return err
	}

	logger.Infoln("Catalog service removed successfully")

	return nil
}

// We are currently associating secret names with pods via pod labels and relying on those labels for secret cleanup.
// Since this is not an ideal approach for managing secret deletion, we should design a more robust and reliable mechanism in the future.
// fetchSecretsToDelete fetches the secrets to delete and secrets which are to be deleted when --skip-cleanup is not set.
func fetchSecretsToDelete(pods []types.Pod) ([]string, []string) {
	secretMapToDelete := make(map[string]bool)
	secretMapToSkip := make(map[string]bool)

	for _, pod := range pods {
		// fetch secret name from pod labels
		if secretNames, ok := pod.Labels[catalogConstants.CatalogSecretLabel]; ok && secretNames != "" {
			// check if it has skip-cleanup label
			_, hasSkipLabel := pod.Labels[catalogConstants.CatalogSecretSkipLabel]

			secrets := strings.Split(secretNames, ",")
			for _, secretName := range secrets {
				secretName = strings.TrimSpace(secretName)
				if secretName != "" {
					if hasSkipLabel {
						secretMapToSkip[secretName] = true
					} else {
						secretMapToDelete[secretName] = true
					}
				}
			}
		}
	}

	secretsToDelete := make([]string, 0, len(secretMapToDelete))
	for secretName := range secretMapToDelete {
		secretsToDelete = append(secretsToDelete, secretName)
	}

	secretsToSkip := make([]string, 0, len(secretMapToSkip))
	for secretName := range secretMapToSkip {
		secretsToSkip = append(secretsToSkip, secretName)
	}

	return secretsToDelete, secretsToSkip
}

// Made with Bob
