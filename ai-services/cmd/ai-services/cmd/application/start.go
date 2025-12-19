package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	skipLogs      bool
	startPodNames []string
)

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start an application",
	Long: `Starts an application by name.

Arguments
  [name]: Application name (required)

Note: Logs are streamed only when a single pod is specified, and only after the pod has started.
`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		startPodNames, err = cmd.Flags().GetStringSlice("pod")
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

		return startApplication(runtimeClient, applicationName, startPodNames)
	},
}

func init() {
	startCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to start (optional)\nCan be specified multiple times: --pod pod1 --pod pod2\nOr comma-separated: --pod pod1,pod2")
	startCmd.Flags().BoolVar(&skipLogs, "skip-logs", false, "Skip displaying logs after starting the pod")
}

// startApplication starts all pods associated with the given application name
func startApplication(client *podman.PodmanClient, appName string, podNames []string) error {
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
		1. If pod names are provided, filter the pods to only include those names.
		2. Warn the user if any provided pod names do not exist.
		3. If no pod names are provided, proceed to start only those pods which don't have the "ai-services.io/start=off" label set.
		4. However, if pod names are provided, ignore the "ai-services.io/start=off" annotation and attempt to start the specified pods.
	*/

	var podsToStart []*types.ListPodsReport
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
				podsToStart = append(podsToStart, pod)
			} else {
				notFound = append(notFound, podname)
			}
		}

		// 2. Warn if any provided pod names do not exist
		if len(notFound) > 0 {
			logger.Warningf("The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
		}

		if len(podsToStart) == 0 {
			logger.Infof("No valid pods found to start for application: %s\n", appName)

			return nil
		}
	} else {
		// 3. No pod names provided, start pods based on annotation
	outerloop:
		for _, pod := range pods {
			for _, container := range pod.Containers {
				// inspect one of containers to get pod annotations
				data, err := client.InspectContainer(container.Names)
				if err != nil {
					return fmt.Errorf("failed to inspect container %s: %w", container.Names, err)
				}
				annotations := data.Config.Annotations
				if val, exists := annotations[constants.PodStartAnnotationkey]; exists && val == constants.PodStartOff {
					continue outerloop
				}
			}
			podsToStart = append(podsToStart, pod)
		}
	}

	if len(podsToStart) == 0 {
		logger.Infoln("No pods available to start.")

		return nil
	}

	logger.Infof("Found %d pods for given applicationName: %s.\n", len(podsToStart), appName)
	logger.Infoln("Below pods will be started:")
	for _, pod := range podsToStart {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	printLogs := len(podsToStart) == 1 && !skipLogs
	if printLogs {
		logger.Infoln("Note: After starting the pod, logs will be displayed. Press Ctrl+C to exit the logs and return to the terminal.")
	}

	confirmStart, err := utils.ConfirmAction("Are you sure you want to start above pods? ")
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmStart {
		logger.Infoln("Skipping starting of pods")

		return nil
	}

	logger.Infoln("Proceeding to start pods...")

	// 3. Proceed to start only the valid pods
	var errors []string
	for _, pod := range podsToStart {
		logger.Infof("Starting the pod: %s\n", pod.Name)
		podData, err := client.InspectPod(pod.Name)
		if err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)

			continue
		}

		if podData.State == "Running" {
			logger.Infof("Pod %s is already running. Skipping...\n", pod.Name)

			continue
		}
		if err := client.StartPod(pod.Id); err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)

			continue
		}
		logger.Infof("Successfully started the pod: %s\n", pod.Name)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to start pods: \n%s", strings.Join(errors, "\n"))
	}

	if printLogs {
		logger.Infof("\n--- Following logs for pod: %s ---\n", podsToStart[0].Name)
		if err := client.PodLogs(podsToStart[0].Name); err != nil {
			// Check if error is due to interrupt signal (Ctrl+C)
			if strings.Contains(err.Error(), "signal: interrupt") || strings.Contains(err.Error(), "context canceled") {
				logger.Infoln("Log following stopped.")

				return nil
			}

			return fmt.Errorf("failed to follow logs for pod %s: %w", podsToStart[0].Name, err)
		}
	}

	return nil
}
