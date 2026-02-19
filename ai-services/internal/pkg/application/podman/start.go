package podman

import (
	"fmt"
	"strings"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Start starts a stopped application.
func (p *PodmanApplication) Start(opts appTypes.StartOptions) error {
	pods, err := p.fetchPodsFromRuntime(opts.Name)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		logger.Infof("No pods found with given application: %s\n", opts.Name)

		return nil
	}

	// Filter pods based on provided pod names or annotation
	podsToStart, err := p.fetchPodsToStart(pods, opts.PodNames)
	if err != nil {
		return err
	}
	if len(podsToStart) == 0 {
		logger.Infof("Invalid/No pods found to start for given application: %s\n", opts.Name)

		return nil
	}

	return p.confirmAndStartPods(podsToStart, opts.AutoYes, opts.SkipLogs)
}

// Start implementation helper methods.
func (p *PodmanApplication) fetchPodsFromRuntime(appName string) ([]types.Pod, error) {
	pods, err := p.runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, nil
}

func (p *PodmanApplication) fetchPodsToStart(pods []types.Pod, podNames []string) ([]types.Pod, error) {
	if len(podNames) > 0 {
		return p.filterPodsByNameForStart(pods, podNames)
	}
	// No pod names provided, start pods based on annotation
	return p.filterPodsByAnnotationForStart(pods)
}

func (p *PodmanApplication) confirmAndStartPods(podsToStart []types.Pod, autoYes, skipLogs bool) error {
	p.logPodsToStart(podsToStart)
	printLogs := p.shouldPrintLogs(podsToStart, skipLogs)

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

	if err := p.startPods(podsToStart); err != nil {
		return err
	}

	if printLogs {
		if err := p.printPodLogs(podsToStart); err != nil {
			return err
		}
	}

	return nil
}

func (p *PodmanApplication) logPodsToStart(podsToStart []types.Pod) {
	logger.Infof("Found %d pods for given applicationName.\n", len(podsToStart))
	logger.Infoln("Below pods will be started:")
	for _, pod := range podsToStart {
		logger.Infof("\t-> %s\n", pod.Name)
	}
}

func (p *PodmanApplication) shouldPrintLogs(podsToStart []types.Pod, skipLogs bool) bool {
	if len(podsToStart) != 1 || skipLogs {
		return false
	}
	logger.Infoln("Note: After starting the pod, logs will be displayed. Press Ctrl+C to exit the logs and return to the terminal.")

	return true
}

func (p *PodmanApplication) startPods(podsToStart []types.Pod) error {
	var errors []string
	for _, pod := range podsToStart {
		logger.Infof("Starting the pod: %s\n", pod.Name)
		podData, err := p.runtime.InspectPod(pod.Name)
		if err != nil {
			errMsg := fmt.Sprintf("%s: %v", pod.Name, err)
			errors = append(errors, errMsg)

			continue
		}

		if podData.State == "Running" {
			logger.Infof("Pod %s is already running. Skipping...\n", pod.Name)

			continue
		}
		if err := p.runtime.StartPod(pod.ID); err != nil {
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

func (p *PodmanApplication) printPodLogs(podsToStart []types.Pod) error {
	logger.Infof("\n--- Following logs for pod: %s ---\n", podsToStart[0].Name)

	if err := p.runtime.PodLogs(podsToStart[0].Name); err != nil {
		if strings.Contains(err.Error(), "signal: interrupt") || strings.Contains(err.Error(), "context canceled") {
			logger.Infoln("Log following stopped.")

			return nil
		}

		return fmt.Errorf("failed to follow logs for pod %s: %w", podsToStart[0].Name, err)
	}

	return nil
}

func (p *PodmanApplication) filterPodsByNameForStart(pods []types.Pod, podNames []string) ([]types.Pod, error) {
	podMap := make(map[string]types.Pod)
	for _, pod := range pods {
		podMap[pod.Name] = pod
	}

	var notFound []string
	var podsToStart []types.Pod
	for _, podName := range podNames {
		if pod, exists := podMap[podName]; exists {
			podsToStart = append(podsToStart, pod)
		} else {
			notFound = append(notFound, podName)
		}
	}

	if len(notFound) > 0 {
		logger.Warningf("The following specified pods were not found and will be skipped: %s\n", strings.Join(notFound, ", "))
	}

	return podsToStart, nil
}

func (p *PodmanApplication) filterPodsByAnnotationForStart(pods []types.Pod) ([]types.Pod, error) {
	var podsToStart []types.Pod

outerloop:
	for _, pod := range pods {
		for _, container := range pod.Containers {
			data, err := p.runtime.InspectContainer(container.Name)
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
