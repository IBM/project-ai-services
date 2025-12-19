package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	stopPodNames []string
)

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stops the running application",
	Long: `Stops a running application by name.

Arguments
  [name]: Application name (required)
`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		stopPodNames, err = cmd.Flags().GetStringSlice("pod")
		if err != nil {
			return fmt.Errorf("failed to parse --pod flag: %w", err)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		return stopApplication(runtimeClient, applicationName, stopPodNames)
	},
}

func init() {
	stopCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to stop (optional)\nCan be specified multiple times: --pod pod1 --pod pod2\nOr comma-separated: --pod pod1,pod2")
}

// stopApplication stops all pods associated with the given application name
func stopApplication(client *podman.PodmanClient, appName string, podNames []string) error {
	resp, err := client.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	var pods []*types.ListPodsReport
	if val, ok := resp.([]*types.ListPodsReport); ok {
		pods = val
	}

	if len(pods) == 0 {
		logger.Infof("No pods found with given application: %s\n", appName)

		return nil
	}

	/*
		1. Filter pods based on provided pod names, as we want to stop only those
		2. Warn if any provided pod names do not exist
		3. Proceed to stop only the valid pods
	*/

	var podsToStop []*types.ListPodsReport
	if len(podNames) > 0 {
		// 1. Filter pods
		podMap := make(map[string]*types.ListPodsReport)
		for _, pod := range pods {
			podMap[pod.Name] = pod
		}

		// maintain list of not found pod names
		var notFound []string
		for _, podname := range podNames {
			if pod, exists := podMap[podname]; exists {
				podsToStop = append(podsToStop, pod)
			} else {
				notFound = append(notFound, podname)
			}
		}

		// 2. Warn if any provided pod names do not exist
		if len(notFound) > 0 {
			logger.Warningf("The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
		}

		if len(podsToStop) == 0 {
			logger.Infof("No valid pods found to stop for application: %s\n", appName)

			return nil
		}
	} else {
		// No specific pod names provided, stop all pods
		podsToStop = pods
	}

	logger.Infof("Found %d pods for given applicationName: %s.\n", len(podsToStop), appName)
	logger.Infoln("Below pods will be stopped:")
	for _, pod := range podsToStop {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	confirmStop, err := utils.ConfirmAction("Are you sure you want to stop the above pods? ")
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmStop {
		logger.Infof("Skipping stopping of pods\n")

		return nil
	}

	logger.Infof("Proceeding to stop pods...\n")

	// 3. Proceed to stop only the valid pods
	var errors []string
	for _, pod := range podsToStop {
		logger.Infof("Stopping the pod: %s\n", pod.Name)
		if err := client.StopPod(pod.Id); err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)

			continue
		}
		logger.Infof("Successfully stopped the pod: %s\n", pod.Name)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to stop pods: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}
