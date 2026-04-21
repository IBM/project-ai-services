package podman

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
)

const (
	containerCreationTimeout       = 5 * time.Minute
	extraContainerReadinessTimeout = 2 * time.Minute
)

// DeployPodAndReadinessCheck deploys a pod and performs readiness checks on its containers.
func DeployPodAndReadinessCheck(rt runtime.Runtime, podSpec *models.PodSpec,
	podTemplateName string, body io.Reader, opts map[string]string) error {
	pods, err := rt.CreatePod(body, opts)
	if err != nil {
		return fmt.Errorf("failed pod creation: %w", err)
	}

	logger.Infof("'%s': Successfully ran podman kube play\n", podTemplateName, logger.VerbosityLevelDebug)

	// ---- Pod Readiness Checks ----
	for _, pod := range pods {
		pInfo, err := rt.InspectPod(pod.ID)
		if err != nil {
			return fmt.Errorf("failed to do pod inspect for podID: '%s' with error: %w", pod.ID, err)
		}

		podName := pInfo.Name

		logger.Infof("'%s', '%s': Starting Pod Readiness check...\n", podTemplateName, podName)

		// Step1: ---- Containers Creation Check ----
		if err := doContainersCreationCheck(rt, podSpec, podTemplateName, pInfo.Name, pInfo.ID); err != nil {
			return err
		}

		// Step2: ---- Containers Readiness Check ----
		for _, container := range pInfo.Containers {
			if err := doContainerReadinessCheck(rt, podTemplateName, pInfo.Name, container.ID); err != nil {
				return err
			}
			logger.Infoln("-------")
		}
		logger.Infof("'%s', '%s': Pod has been successfully deployed and ready!\n", podTemplateName, podName)
		logger.Infoln("-------")
	}

	logger.Infoln("-------\n-------")

	return nil
}

func doContainersCreationCheck(rt runtime.Runtime, podSpec *models.PodSpec, podTemplateName, podName, podID string) error {
	logger.Infof("'%s', '%s': Performing Containers Creation check for pod...\n", podTemplateName, podName)

	expectedContainerCount := len(specs.FetchContainerNames(*podSpec))

	logger.Infof("'%s', '%s': Waiting for Containers Creation... Timeout set: %s\n", podTemplateName, podName, containerCreationTimeout)
	// wait for all containers for a given pod are created
	if err := helpers.WaitForContainersCreation(rt, podID, expectedContainerCount, containerCreationTimeout); err != nil {
		return fmt.Errorf("containers creation check failed for pod: '%s' with error: %w", podName, err)
	}

	logger.Infof("'%s', '%s': Containers creation check for pod is completed\n", podTemplateName, podName)

	return nil
}

func doContainerReadinessCheck(rt runtime.Runtime, podTemplateName, podName, containerID string) error {
	cInfo, err := rt.InspectContainer(containerID)
	if err != nil {
		return fmt.Errorf("failed to do container inspect for containerID: '%s' with error: %w", containerID, err)
	}

	logger.Infof("'%s', '%s', '%s': Performing Container Readiness check...\n", podTemplateName, podName, cInfo.Name)

	// getting the Start Period set for a container
	startPeriod, err := helpers.FetchContainerStartPeriod(rt, containerID)
	if err != nil {
		return fmt.Errorf("fetching container: '%s' start period failed: %w", cInfo.Name, err)
	}

	if startPeriod == -1 {
		logger.Infof("No container health check is set for '%s'. Hence skipping readiness check\n", cInfo.Name, logger.VerbosityLevelDebug)

		return nil
	}

	// configure readiness timeout by appending start period with additional extra timeout
	readinessTimeout := startPeriod + extraContainerReadinessTimeout

	logger.Infof("'%s', '%s', '%s': Waiting for Container Readiness... Timeout set: %s\n", podTemplateName, podName, cInfo.Name, readinessTimeout)

	if err := helpers.WaitForContainerReadiness(rt, containerID, readinessTimeout); err != nil {
		return fmt.Errorf("readiness check failed for container: '%s'!: %w", cInfo.Name, err)
	}
	logger.Infof("'%s', '%s', '%s': Readiness Check for the container is completed!\n", podTemplateName, podName, cInfo.Name)

	return nil
}

// ConstructPodDeployOptions constructs pod deployment options from annotations.
func ConstructPodDeployOptions(podAnnotations map[string]string) map[string]string {
	podStart := checkForPodStartAnnotation(podAnnotations)

	// construct start option
	podDeployOptions := map[string]string{}
	if podStart != "" {
		podDeployOptions["start"] = podStart
	}

	// construct publish option
	hostPortMappings := fetchHostPortMappingFromAnnotation(podAnnotations)
	podDeployOptions["publish"] = ""

	// loop over each of the hostPortMappings to construct the 'publish' option
	for containerPort, hostPort := range hostPortMappings {
		if hostPort == "0" {
			// if the host port is set to 0, then do not expose the particular containerPort
			continue
		}
		if hostPort != "" {
			// if the host port is present
			podDeployOptions["publish"] += hostPort + ":" + containerPort
		} else {
			// else just populate the containerPort, so that dynamically podman will populate
			podDeployOptions["publish"] += containerPort
		}
		podDeployOptions["publish"] += ","
	}

	return podDeployOptions
}

func checkForPodStartAnnotation(podAnnotations map[string]string) string {
	if val, ok := podAnnotations[constants.PodStartAnnotationkey]; ok {
		if val == constants.PodStartOff || val == constants.PodStartOn {
			return val
		}
	}

	return ""
}

func fetchHostPortMappingFromAnnotation(podAnnotations map[string]string) map[string]string {
	// key -> containerPort and value -> hostPort
	hostPortMapping := map[string]string{}

	portMappings, ok := podAnnotations[constants.PodPortsAnnotationKey]
	if !ok {
		// return empty map if port annotation is not present
		return hostPortMapping
	}

	portMapping := strings.SplitSeq(portMappings, ",")
	for p := range portMapping {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Find colon
		i := strings.Index(p, ":")
		if i == -1 {
			// No colon → whole thing is the containerPort
			hostPortMapping[p] = ""

			continue
		}

		// Before colon string is hostPort
		hostPort := strings.TrimSpace(p[:i])
		// After colon string is containerPort
		containerPort := strings.TrimSpace(p[i+1:])

		// If colon exists but NO value after the colon (containerPort) → then skip
		if containerPort == "" {
			continue
		}

		hostPortMapping[containerPort] = hostPort
	}

	return hostPortMapping
}

// Made with Bob
