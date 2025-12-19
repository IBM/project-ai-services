package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

var (
	skipCleanup bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete an application",
	Long: `Deletes an application and all associated resources.

Arguments
  [name]: Application name (required)`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

		return utils.VerifyAppName(appName)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		// podman connectivity
		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		err = deleteApplication(runtimeClient, applicationName)
		if err != nil {
			return fmt.Errorf("failed to delete application: %w", err)
		}

		return nil

	},
}

func init() {
	deleteCmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "Skip deleting application data (default=false)")
}

func deleteApplication(client *podman.PodmanClient, appName string) error {
	appDir := filepath.Join(constants.ApplicationsPath, filepath.Base(appName))
	appExists := false

	if _, err := os.Stat(appDir); err == nil {
		appExists = true
	}

	resp, err := client.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// TODO: Avoid doing the type assertion and importing types package from podman

	var pods []*types.ListPodsReport
	if val, ok := resp.([]*types.ListPodsReport); ok {
		pods = val
	}

	podsExists := len(pods) != 0

	if podsExists {
		logger.Infof("Found %d pods for given applicationName: %s.\n", len(pods), appName)
		logger.Infoln("Below are the list of pods to be deleted")
		for _, pod := range pods {
			logger.Infof("\t-> %s\n", pod.Name)
		}
	} else {
		logger.Infof("No pods found for application: %s\n", appName)
	}

	var confirmActionPrompt string
	if podsExists && appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the above pods and application data? "
	} else if podsExists {
		confirmActionPrompt = "Are you sure you want to delete the above pods? "
	} else if appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the application data? "
	} else {
		logger.Infof("Application %s does not exist", appName)

		return nil
	}

	confirmDelete, err := utils.ConfirmAction(confirmActionPrompt)
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmDelete {
		logger.Infoln("Deletion cancelled")

		return nil
	}

	logger.Infoln("Proceeding with deletion...")

	var errors []string
	if podsExists {
		for _, pod := range pods {
			logger.Infof("Deleting pod: %s\n", pod.Name)
			if err := client.DeletePod(pod.Id, utils.BoolPtr(true)); err != nil {
				errors = append(errors, fmt.Sprintf("pod %s: %v", pod.Name, err))

				continue
			}
			logger.Infof("Successfully removed pod: %s\n", pod.Name)
		}
	}

	// Aggregate errors at the end
	if len(errors) > 0 {
		return fmt.Errorf("failed to remove pods: \n%s", strings.Join(errors, "\n"))
	}

	if appExists && !skipCleanup {
		logger.Infoln("Cleaning up application data")
		if err := os.RemoveAll(appDir); err != nil {
			return fmt.Errorf("failed to delete application data: %w", err)
		}
		logger.Infoln("Application data cleaned up successfully")
	}

	return nil
}
