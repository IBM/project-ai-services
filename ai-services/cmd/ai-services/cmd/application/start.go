package application

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	skipLogs      bool
	startPodNames []string
	autoYes       bool
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
	startCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
}

// startApplication starts all pods associated with the given application name.
func startApplication(client *podman.PodmanClient, appName string, podNames []string) error {
	pods, err := fetchPodsFromRuntime(client, appName)
	if err != nil {
		return err
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

	// Do Step 1, Step 2 and Step 3
	podsToStart, err := fetchPodsToStart(client, pods, podNames)
	if err != nil {
		return err
	}
	if len(podsToStart) == 0 {
		logger.Infof("Invalid/No pods found to start for given application: %s\n", appName)

		return nil
	}

	if err := confirmAndStartPods(client, podsToStart); err != nil {
		return err
	}

	return nil
}

func confirmAndStartPods(client *podman.PodmanClient, podsToStart []types.Pod) error {
	logPodsToStart(podsToStart)
	printLogs := shouldPrintLogs(podsToStart)

	if !autoYes {
		confirmStart, err := utils.ConfirmAction("Are you sure you want to start above pods? ")
		if err != nil {
			return fmt.Errorf("failed to take user input: %w", err)
		}
		if !confirmStart {
			logger.Infoln("Skipping starting of pods")

			return nil
		}
	}

	logger.Infoln("Proceeding to start pods...")

	if err := startPods(client, podsToStart); err != nil {
		return err
	}

	if printLogs {
		if err := printPodLogs(client, podsToStart); err != nil {
			return err
		}
	}

	return nil
}

func logPodsToStart(podsToStart []types.Pod) {
	logger.Infof("Found %d pods for given applicationName.\n", len(podsToStart))
	logger.Infoln("Below pods will be started:")
	for _, pod := range podsToStart {
		logger.Infof("\t-> %s\n", pod.Name)
	}
}

func shouldPrintLogs(podsToStart []types.Pod) bool {
	// if there are more than 1 pod to be started or if skip-logs flag is set, then skip printing logs
	if len(podsToStart) != 1 || skipLogs {
		return false
	}

	logger.Infoln("Note: After starting the pod, logs will be displayed. Press Ctrl+C to exit the logs and return to the terminal.")

	return true
}

func fetchPodsFromRuntime(client *podman.PodmanClient, appName string) ([]types.Pod, error) {
	pods, err := client.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, err
}

func fetchPodsToStart(client *podman.PodmanClient, pods []types.Pod, podNames []string) ([]types.Pod, error) {
	if len(podNames) > 0 {
		return filterPodsByName(pods, podNames)
	}

	// No pod names provided, start pods based on annotation
	return filterPodsByAnnotation(client, pods)
}

func startPods(client *podman.PodmanClient, podsToStart []types.Pod) error {
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
		if err := client.StartPod(pod.ID); err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)

			continue
		}

		logger.Infof("Successfully started the pod: %s\n", pod.Name)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to start pods: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}

func printPodLogs(client *podman.PodmanClient, podsToStart []types.Pod) error {
	logger.Infof("\n--- Following logs for pod: %s ---\n", podsToStart[0].Name)

	if err := client.PodLogs(podsToStart[0].Name); err != nil {
		// Check if error is due to interrupt signal (Ctrl+C)
		if strings.Contains(err.Error(), "signal: interrupt") || strings.Contains(err.Error(), "context canceled") {
			logger.Infoln("Log following stopped.")

			return nil
		}

		return fmt.Errorf("failed to follow logs for pod %s: %w", podsToStart[0].Name, err)
	}

	return nil
}

func filterPodsByName(pods []types.Pod, podNames []string) ([]types.Pod, error) {
	// 1. Filter pods
	podMap := make(map[string]types.Pod)
	for _, pod := range pods {
		podMap[pod.Name] = pod
	}

	// maintain list of not found pod names
	var notFound []string
	var podsToStart []types.Pod
	for _, podName := range podNames {
		if pod, exists := podMap[podName]; exists {
			podsToStart = append(podsToStart, pod)
		} else {
			notFound = append(notFound, podName)
		}
	}

	// 2. Warn if any provided pod names do not exist
	if len(notFound) > 0 {
		logger.Warningf("The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
	}

	return podsToStart, nil
}

func filterPodsByAnnotation(client *podman.PodmanClient, pods []types.Pod) ([]types.Pod, error) {
	var podsToStart []types.Pod

outerloop:
	for _, pod := range pods {
		for _, container := range pod.Containers {
			// inspect one of containers to get pod annotations
			data, err := client.InspectContainer(container.Name)
			if err != nil {
				return podsToStart, fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
			}
			annotations := data.Annotations
			if val, exists := annotations[constants.PodStartAnnotationkey]; exists && val == constants.PodStartOff {
				continue outerloop
			}
		}
		podsToStart = append(podsToStart, pod)
	}

	return podsToStart, nil
}
