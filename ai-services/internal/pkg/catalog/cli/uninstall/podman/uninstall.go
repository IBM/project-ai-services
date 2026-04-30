package podman

import (
	"context"
	"fmt"
	"os"
	"strings"

	catalog "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	// Default database data path from catalog values.yaml
	defaultDBDataPath = "/var/lib/ai-services/db"
)

// UninstallCatalog removes the catalog service and all associated resources.
func UninstallCatalog(ctx context.Context, autoYes, skipCleanup bool) error {
	s := spinner.New("Checking catalog service...")
	s.Start(ctx)

	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		s.Fail("failed to initialize podman client")

		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Check if catalog pods exist
	pods, err := rt.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", catalog.CatalogAppName)},
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	podsExists := len(pods) != 0

	if !podsExists {
		s.Stop("No catalog service found")
		logger.Infoln("Catalog service is not deployed")

		return nil
	}

	s.Stop(fmt.Sprintf("Found %d catalog pod(s)", len(pods)))

	// Print pods to be deleted
	logPodsToBeDeleted(pods)

	// Confirm deletion if not auto-yes
	if !autoYes {
		confirmed, err := deleteConfirmation()
		if err != nil {
			return err
		}
		if !confirmed {
			logger.Infoln("Deletion cancelled")

			return nil
		}
	}

	logger.Infoln("Proceeding with deletion...")

	// Delete catalog pods
	if err := podsDeletion(rt, pods); err != nil {
		return err
	}

	// Delete catalog secret
	if err := secretDeletion(rt); err != nil {
		return err
	}

	if !skipCleanup {
		// Delete database data
		if err := dbDataDeletion(); err != nil {
			return err
		}
	} else {
		logger.Infoln("Skipping database data cleanup (--skip-cleanup flag set)", logger.VerbosityLevelDebug)
	}

	logger.Infoln("Catalog service removed successfully")

	return nil
}

// logPodsToBeDeleted prints the list of pods that will be deleted.
func logPodsToBeDeleted(pods []types.Pod) {
	logger.Infoln("Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}
}

// deleteConfirmation prompts the user to confirm deletion.
func deleteConfirmation() (bool, error) {
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	return confirmed, nil
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

// secretDeletion removes the catalog secret.
func secretDeletion(rt *podman.PodmanClient) error {
	secrets, err := rt.ListSecrets(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", catalog.CatalogAppName)},
	})
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	secretsExists := len(secrets) != 0

	if !secretsExists {
		logger.Infof("No secrets found for: %s\n", catalog.CatalogAppName)

		return nil
	}

	for _, secret := range secrets {
		err := rt.DeleteSecret(secret)
		if err != nil {
			return fmt.Errorf("failed to remove secret: %w", err)
		}
	}

	return nil
}

// dbDataDeletion removes the database data directory.
func dbDataDeletion() error {
	// Check if database data directory exists
	if _, err := os.Stat(defaultDBDataPath); os.IsNotExist(err) {
		logger.Infof("Database data directory does not exist: %s\n", defaultDBDataPath)

		return nil
	}

	logger.Infof("\nDatabase data found at: %s\n", defaultDBDataPath, logger.VerbosityLevelDebug)

	logger.Infof("Deleting database data at: %s\n", defaultDBDataPath)

	// Remove the database data directory
	if err := os.RemoveAll(defaultDBDataPath); err != nil {
		return fmt.Errorf("failed to remove database data directory: %w", err)
	}

	logger.Infof("Successfully removed database data at: %s\n", defaultDBDataPath)

	return nil
}

// Made with Bob
