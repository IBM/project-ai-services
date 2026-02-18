package podman

import (
	"fmt"
	"strings"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Stop stops a running application.
func (p *PodmanApplication) Stop(opts appTypes.StopOptions) error {
	pods, err := p.runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", opts.Name)},
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infof("No pods found with given application: %s\n", opts.Name)

		return nil
	}

	// Filter pods based on provided pod names
	podsToStop, err := p.fetchPodsToStop(pods, opts.PodNames, opts.Name)
	if err != nil {
		return err
	}

	if len(podsToStop) == 0 {
		logger.Infof("Invalid/No pods found to stop for given application: %s\n", opts.Name)

		return nil
	}

	logger.Infof("Found %d pods for given applicationName: %s.\n", len(podsToStop), opts.Name)
	logger.Infoln("Below pods will be stopped:")
	for _, pod := range podsToStop {
		logger.Infof("\t-> %s\n", pod.Name)
	}

	if !opts.AutoYes {
		confirmStop, err := utils.ConfirmAction("Are you sure you want to stop the above pods? ")
		if err != nil {
			return fmt.Errorf("failed to take user input: %w", err)
		}

		if !confirmStop {
			logger.Infof("Skipping stopping of pods\n")

			return nil
		}
	}

	logger.Infof("Proceeding to stop pods...\n")

	return p.stopPods(podsToStop)
}

func (p *PodmanApplication) fetchPodsToStop(pods []types.Pod, podNames []string, appName string) ([]types.Pod, error) {
	var podsToStop []types.Pod
	if len(podNames) > 0 {
		// Filter pods
		podMap := make(map[string]types.Pod)
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

		// Warn if any provided pod names do not exist
		if len(notFound) > 0 {
			logger.Warningf("The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
		}
	} else {
		// No specific pod names provided, stop all pods
		podsToStop = pods
	}

	return podsToStop, nil
}

func (p *PodmanApplication) stopPods(podsToStop []types.Pod) error {
	var errors []string
	for _, pod := range podsToStop {
		logger.Infof("Stopping the pod: %s\n", pod.Name)

		if err := p.runtime.StopPod(pod.ID); err != nil {
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
