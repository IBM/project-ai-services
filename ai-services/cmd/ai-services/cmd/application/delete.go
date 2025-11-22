package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Deletes the running application",
	Long: `Deletes the running application based on the application name
		Arguments
		- [name]: Application name (Required)
	`,
	Args: cobra.ExactArgs(1),
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

func deleteApplication(client *podman.PodmanClient, appName string) error {
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

	if len(pods) == 0 {
		logger.Infof("No pods found with given application: %s\n", appName)
		return nil
	}

	logger.Infof("Found %d pods for given applicationName: %s.\n", len(pods), appName)
	logger.Infoln("Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	logger.Infof("Are you sure you want to delete above pods? (y/N): ")

	confirmDelete, err := utils.ConfirmAction()
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmDelete {
		logger.Infof("Skipping the deletion of pods")
		return nil
	}

	logger.Infof("Proceeding with deletion...\n")

	// Loop over each of the pods and call delete
	var errors []string
	for _, pod := range pods {
		logger.Infof("Deleting the pod: %s\n", pod.Name)
		if err := client.DeletePod(pod.Id, utils.BoolPtr(true)); err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)
			continue
		}
		logger.Infof("Successfully removed the pod: %s\n", pod.Name)
	}

	// Aggregate errors at the end
	if len(errors) > 0 {
		return fmt.Errorf("failed to remove pods: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}
