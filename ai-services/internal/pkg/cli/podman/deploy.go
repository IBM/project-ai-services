package podman

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	v1 "github.com/containers/podman/v5/pkg/k8s.io/api/core/v1"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
)

const (
	containerCreationTimeout = 5 * time.Minute
	// readinessTimeoutBuffer is added on top of the probe-derived window as a small
	// safety margin for scheduling jitter between probe cycles.
	readinessTimeoutBuffer = 30 * time.Second

	// k8s defaults used when the probe omits these fields.
	defaultProbePeriodSeconds    = 10
	defaultProbeFailureThreshold = 3
)

// DeployPodAndReadinessCheck deploys a pod and performs readiness checks on its containers.
func DeployPodAndReadinessCheck(ctx context.Context, rt runtime.Runtime, podSpec *models.PodSpec,
	podTemplateName string, body io.Reader, opts map[string]string) error {
	pods, err := rt.CreatePod(ctx, body, opts)
	if err != nil {
		return fmt.Errorf("failed pod creation: %w", err)
	}

	logger.DebugfCtx(ctx, "'%s': Successfully ran podman kube play\n", podTemplateName)

	// ---- Pod Readiness Checks ----
	for _, pod := range pods {
		pInfo, err := rt.InspectPod(ctx, pod.ID)
		if err != nil {
			return fmt.Errorf("failed to do pod inspect for podID: '%s' with error: %w", pod.ID, err)
		}

		podName := pInfo.Name

		logger.InfofCtx(ctx, "'%s', '%s': Starting Pod Readiness check...\n", podTemplateName, podName)

		// Step1: ---- Containers Creation Check ----
		if err := doContainersCreationCheck(ctx, rt, podSpec, podTemplateName, pInfo.Name, pInfo.ID); err != nil {
			return err
		}

		// Step2: ---- Containers Readiness Check ----
		for _, container := range pInfo.Containers {
			if err := doContainerReadinessCheck(ctx, rt, podSpec, podTemplateName, pInfo.Name, container.ID); err != nil {
				return err
			}
			logger.InfolnCtx(ctx, "-------")
		}
		logger.InfofCtx(ctx, "'%s', '%s': Pod has been successfully deployed and ready!\n", podTemplateName, podName)
		logger.InfolnCtx(ctx, "-------")
	}

	logger.InfolnCtx(ctx, "-------\n-------")

	return nil
}

func doContainersCreationCheck(ctx context.Context, rt runtime.Runtime, podSpec *models.PodSpec, podTemplateName, podName, podID string) error {
	logger.InfofCtx(ctx, "'%s', '%s': Performing Containers Creation check for pod...\n", podTemplateName, podName)

	expectedContainerCount := len(specs.FetchContainerNames(*podSpec))

	logger.InfofCtx(ctx, "'%s', '%s': Waiting for Containers Creation... Timeout set: %s\n", podTemplateName, podName, containerCreationTimeout)
	// wait for all containers for a given pod are created
	if err := helpers.WaitForContainersCreation(ctx, rt, podID, expectedContainerCount, containerCreationTimeout); err != nil {
		return fmt.Errorf("containers creation check failed for pod: '%s' with error: %w", podName, err)
	}

	logger.InfofCtx(ctx, "'%s', '%s': Containers creation check for pod is completed\n", podTemplateName, podName)

	return nil
}

func doContainerReadinessCheck(ctx context.Context, rt runtime.Runtime, podSpec *models.PodSpec, podTemplateName, podName, containerID string) error {
	cInfo, err := rt.InspectContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to do container inspect for containerID: '%s' with error: %w", containerID, err)
	}

	logger.InfofCtx(ctx, "'%s', '%s', '%s': Performing Container Readiness check...\n", podTemplateName, podName, cInfo.Name)

	// Resolve the liveness probe for this container from the pod spec. The probe
	// carries the authoritative timing values (initialDelaySeconds, periodSeconds,
	// failureThreshold) that we must honour when computing the readiness timeout.
	probe := fetchLivenessProbe(podSpec, cInfo.Name)

	if probe == nil {
		// No healthcheck configured — skip readiness wait entirely.
		logger.DebugfCtx(ctx, "No liveness probe configured for '%s'. Skipping readiness check\n", cInfo.Name)

		return nil
	}

	readinessTimeout, pollInterval := computeReadinessTimeout(probe)

	logger.InfofCtx(ctx, "'%s', '%s', '%s': Waiting for Container Readiness... Timeout set: %s\n", podTemplateName, podName, cInfo.Name, readinessTimeout)

	if err := helpers.WaitForContainerReadiness(ctx, rt, containerID, readinessTimeout, pollInterval); err != nil {
		return fmt.Errorf("readiness check failed for container: '%s'!: %w", cInfo.Name, err)
	}
	logger.InfofCtx(ctx, "'%s', '%s', '%s': Readiness Check for the container is completed!\n", podTemplateName, podName, cInfo.Name)

	return nil
}

// fetchLivenessProbe returns the LivenessProbe for the named container in the pod spec,
// or nil if no probe is defined or the container is not found (e.g. the infra container).
//
// Podman names running containers as "<podname>-<specname>" (e.g. "llm-5c32c9e93e-llm"
// for a spec container named "llm" inside pod "llm-5c32c9e93e"). The containerName
// passed here comes from InspectContainer, so we strip the pod name prefix before
// matching against the spec.
func fetchLivenessProbe(podSpec *models.PodSpec, containerName string) *v1.Probe {
	if podSpec == nil {
		return nil
	}
	// Build the pod name prefix once so we only allocate the string once.
	podPrefix := podSpec.Name + "-"
	specName := strings.TrimPrefix(containerName, podPrefix)
	for _, c := range podSpec.Spec.Containers {
		if c.Name == specName {
			return c.LivenessProbe
		}
	}

	return nil
}

// computeReadinessTimeout derives the readiness timeout and poll interval from the probe spec.
//
// The maximum time before Kubernetes would declare the container failed is:
//
//	initialDelaySeconds + (failureThreshold × periodSeconds)
//
// We use this as our timeout, plus a small buffer for scheduling jitter.
// The poll interval is set to periodSeconds so we don't call InspectContainer
// more often than Podman evaluates a new probe result.
func computeReadinessTimeout(probe *v1.Probe) (timeout time.Duration, pollInterval time.Duration) {
	initialDelay := time.Duration(probe.InitialDelaySeconds) * time.Second

	periodSecs := int32(defaultProbePeriodSeconds)
	if probe.PeriodSeconds > 0 {
		periodSecs = probe.PeriodSeconds
	}

	failureThreshold := int32(defaultProbeFailureThreshold)
	if probe.FailureThreshold > 0 {
		failureThreshold = probe.FailureThreshold
	}

	pollInterval = time.Duration(periodSecs) * time.Second
	timeout = initialDelay + time.Duration(failureThreshold)*pollInterval + readinessTimeoutBuffer

	return timeout, pollInterval
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
