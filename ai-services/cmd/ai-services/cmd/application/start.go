package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	skipLogs bool
)

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "starts the application",
	Long: `starts the application based on the application name
		Arguments
		- [name]: Application name (Required)
		
		Flags
		- [pod]: Pod name (Optional)
					  Can be specified multiple times: --pod=pod1 --pod=pod2
                      Or comma-separated: --pod=pod1,pod2	
	`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		podnames, err := cmd.Flags().GetStringSlice("pod")
		if err != nil {
			return fmt.Errorf("failed to parse --pod flag: %w", err)
		}

		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		return startApplication(cmd, runtimeClient, applicationName, podnames)
	},
}

func init() {
	startCmd.Flags().StringSlice("pod", []string{}, "Specific pod name(s) to start (optional)")
	startCmd.Flags().BoolVar(&skipLogs, "skip-logs", false, "Skip displaying logs after starting the pod")
}

// startApplication starts all pods associated with the given application name
func startApplication(cmd *cobra.Command, client *podman.PodmanClient, appName string, podnames []string) error {
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
		cmd.Printf("No pods found with given application: %s\n", appName)
		return nil
	}

	/*
		1. Filter pods based on provided pod names, as we want to start only those
		2. Warn if any provided pod names do not exist
		3. Proceed to start only the valid pods
	*/

	var podsToStart []*types.ListPodsReport
	if len(podnames) > 0 {

		// 1. Filter pods
		podMap := make(map[string]*types.ListPodsReport)
		for _, pod := range pods {
			podMap[pod.Name] = pod
		}

		// maintain list of not found pod names
		var notFound []string
		for _, podname := range podnames {
			if pod, exists := podMap[podname]; exists {
				podsToStart = append(podsToStart, pod)
			} else {
				notFound = append(notFound, podname)
			}
		}

		// 2. Warn if any provided pod names do not exist
		if len(notFound) > 0 {
			cmd.Printf("Warning: The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
		}

		if len(podsToStart) == 0 {
			cmd.Printf("No valid pods found to start for application: %s\n", appName)
			return nil
		}
	} else {
		// No specific pod names provided, start all pods
		podsToStart = pods
	}

	cmd.Printf("Found %d pods for given applicationName: %s.\n", len(podsToStart), appName)
	cmd.Println("Below pods will be started:")
	for _, pod := range podsToStart {
		cmd.Printf("\t-> %s\n", pod.Name)
	}

	printLogs := len(podsToStart) == 1 && !skipLogs
	if printLogs {
		cmd.Printf("⚠️  Note: After starting the pod, logs will be displayed. Press Ctrl+C to exit the logs and return to the terminal.\n")
	}
	cmd.Printf("Are you sure you want to start above pods? (y/N): ")

	confirmStart, err := utils.ConfirmAction()
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmStart {
		cmd.Printf("Skipping starting of pods\n")
		return nil
	}

	cmd.Printf("Proceeding to start pods...\n")

	// 3. Proceed to start only the valid pods
	var errors []string
	for _, pod := range podsToStart {
		cmd.Printf("Starting the pod: %s\n", pod.Name)
		podData, err := client.InspectPod(pod.Name)
		if err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)
			continue
		}

		if podData.State == "Running" {
			cmd.Printf("Pod %s is already running. Skipping...\n", pod.Name)
			continue
		}
		if err := client.StartPod(pod.Id); err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)
			continue
		}
		cmd.Printf("Successfully started the pod: %s\n", pod.Name)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to start pods: \n%s", strings.Join(errors, "\n"))
	}

	if printLogs {
		cmd.Printf("\n--- Following logs for pod: %s ---\n", podsToStart[0].Name)
		if err := client.PodLogs(podsToStart[0].Name); err != nil {
			// Check if error is due to interrupt signal (Ctrl+C)
			if strings.Contains(err.Error(), "signal: interrupt") || strings.Contains(err.Error(), "context canceled") {
				cmd.Printf("\nLog following stopped.\n")
				return nil
			}
			return fmt.Errorf("failed to follow logs for pod %s: %w", podsToStart[0].Name, err)
		}
	}

	return nil
}
