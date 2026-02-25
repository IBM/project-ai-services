package podman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Delete removes an application and its associated resources.
func (p *PodmanApplication) Delete(_ context.Context, opts appTypes.DeleteOptions) error {
	appDir := filepath.Join(constants.ApplicationsPath, filepath.Base(opts.Name))
	appExists := utils.FileExists(appDir)

	pods, err := p.runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", opts.Name)},
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	podsExists := len(pods) != 0

	if !podsExists {
		logger.Infof("No pods found for application: %s\n", opts.Name)

		return nil
	}

	// print relevant app pod status
	p.logPodsToBeDeleted(opts.Name, pods)

	if !opts.AutoYes {
		confirmDelete, err := p.deleteConfirmation(opts.Name, podsExists, appExists, opts.SkipCleanup)
		if err != nil {
			return err
		}
		if !confirmDelete {
			logger.Infoln("Deletion cancelled")

			return nil
		}
	}

	logger.Infoln("Proceeding with deletion...")

	if err := p.podsDeletion(pods); err != nil {
		return err
	}

	if appExists && !opts.SkipCleanup {
		if err := p.appDataDeletion(appDir); err != nil {
			return err
		}
	}

	return nil
}

func (p *PodmanApplication) logPodsToBeDeleted(appName string, pods []types.Pod) {
	logger.Infof("Found %d pods for given applicationName: %s.\n", len(pods), appName)
	logger.Infoln("Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}
}

func (p *PodmanApplication) deleteConfirmation(appName string, podsExists, appExists, skipCleanup bool) (bool, error) {
	var confirmActionPrompt string
	if podsExists && appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the above pods and application data? "
	} else if podsExists {
		confirmActionPrompt = "Are you sure you want to delete the above pods? "
	} else if appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the application data? "
	} else {
		logger.Infof("Application %s does not exist", appName)

		return false, nil
	}

	confirmDelete, err := utils.ConfirmAction(confirmActionPrompt)
	if err != nil {
		return confirmDelete, fmt.Errorf("failed to take user input: %w", err)
	}

	return confirmDelete, nil
}

func (p *PodmanApplication) podsDeletion(pods []types.Pod) error {
	var errors []string

	for _, pod := range pods {
		logger.Infof("Deleting pod: %s\n", pod.Name)

		if err := p.runtime.DeletePod(pod.ID, utils.BoolPtr(true)); err != nil {
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

func (p *PodmanApplication) appDataDeletion(appDir string) error {
	logger.Infoln("Cleaning up application data")

	if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("failed to delete application data: %w", err)
	}

	logger.Infoln("Application data cleaned up successfully")

	return nil
}
