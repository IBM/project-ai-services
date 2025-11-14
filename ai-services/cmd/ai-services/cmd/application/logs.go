package application

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/spf13/cobra"
)

var (
	podName           string
	containerNameOrID string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "shows application pod logs",
	Long: `show application pod logs based on pod name		
Flags
- [pod]: Pod name (Required)
- [containter]: Container name (Optional)
Specify container name or ID to show logs of a specific container
	`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if podName == "" {
			return fmt.Errorf("pod name must be specified using --pod flag")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {

		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		return showLogs(runtimeClient, podName, containerNameOrID)
	},
}

func init() {
	logsCmd.Flags().StringVar(&podName, "pod", "", "Pod name to show logs from (required)")
	logsCmd.Flags().StringVar(&containerNameOrID, "container", "", "Container logs to show logs from (Optional)")
	logsCmd.MarkFlagRequired("pod")
}

func showLogs(client *podman.PodmanClient, podName string, containerNameOrID string) error {
	logger.Warningln("Press Ctrl+C to exit the logs and return to the terminal.")
	logger.Infof("Fetching logs for application pod: %s", podName)

	if containerNameOrID != "" {
		exists, err := client.ContainerExists(containerNameOrID)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("container %s doesn't exists", containerNameOrID)
		}
		logger.Infof("Fetching logs for container: %s", containerNameOrID)
		err = client.ContainerLogs(containerNameOrID)
		if err != nil {
			return fmt.Errorf("failed to fetch container: %s logs; err: %w", containerNameOrID, err)
		}
	} else {
		err := client.PodLogs(podName)
		if err != nil {
			return fmt.Errorf("failed to fetch pod: %s logs; err: %w", podName, err)
		}
	}

	return nil
}
