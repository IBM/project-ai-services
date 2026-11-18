package podman

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/uuid"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	k8syaml "sigs.k8s.io/yaml"
)

// RenderAndParsePodTemplate renders a pod template and parses it into a PodSpec.
func RenderAndParsePodTemplate(
	podTemplate *template.Template,
	templateName string,
	params map[string]any,
) (*podmodels.PodSpec, error) {
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", templateName, err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return nil, fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	return &podSpec, nil
}

// RenderFinalPodTemplate renders the final pod template with environment parameters.
// Returns both the PodSpec (for metadata) and the rendered bytes (for deployment).
func RenderFinalPodTemplate(
	podTemplate *template.Template,
	templateName string,
	initialParams map[string]any,
	env map[string]map[string]string,
) (*podmodels.PodSpec, []byte, error) {
	if env != nil {
		initialParams["env"] = env
	}

	var finalRendered bytes.Buffer
	if err := podTemplate.Execute(&finalRendered, initialParams); err != nil {
		return nil, nil, fmt.Errorf("failed to render template %s with env: %w", templateName, err)
	}

	renderedBytes := finalRendered.Bytes()

	var finalPodSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(renderedBytes, &finalPodSpec); err != nil {
		return nil, nil, fmt.Errorf("failed to parse final rendered pod spec: %w", err)
	}

	return &finalPodSpec, renderedBytes, nil
}

// DeployPodmanPod deploys a pod on Podman using the rendered YAML bytes directly.
func DeployPodmanPod(ctx context.Context, rt runtime.Runtime, podSpec *podmodels.PodSpec, renderedBytes []byte, templateName string) error {
	reader := bytes.NewReader(renderedBytes)
	podAnnotations := specs.FetchPodAnnotations(*podSpec)
	podDeployOptions := clipodman.ConstructPodDeployOptions(podAnnotations)

	if err := clipodman.DeployPodAndReadinessCheck(ctx, rt, podSpec, templateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	return nil
}

// ExtractPodEndpoint extracts the internal host (pod name) and port from a component pod spec.
// It reads directly from the container port declarations, which is the authoritative source
// for component-to-service communication. Returns (nil, nil) if the pod does not declare any container port.
func ExtractPodEndpoint(ctx context.Context, podSpec *podmodels.PodSpec) (*EndpointInfo, error) {
	if podSpec == nil {
		return nil, fmt.Errorf("pod spec is nil")
	}

	podName := podSpec.Name
	if podName == "" {
		return nil, fmt.Errorf("pod name is empty")
	}

	var targetPort string
	if len(podSpec.Spec.Containers) > 0 && len(podSpec.Spec.Containers[0].Ports) > 0 {
		targetPort = fmt.Sprintf("%d", podSpec.Spec.Containers[0].Ports[0].ContainerPort)
	}

	if targetPort == "" {
		logger.InfofCtx(ctx, "No container port found in pod spec for %s, skipping endpoint extraction\n", podName)

		return nil, nil
	}

	endpoint := fmt.Sprintf("%s:%s", podName, targetPort)
	logger.InfofCtx(ctx, "Extracted endpoint for %s: %s (domain: %s, port: %s)\n",
		podName, endpoint, podName, targetPort)

	return &EndpointInfo{
		Domain: podName,
		Port:   targetPort,
	}, nil
}

// FetchSpyreCardsFromPodAnnotations extracts Spyre card requirements from pod annotations.
func FetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
	var spyreCards int
	spyreCardContainerMap := map[string]int{}

	isSpyreCardAnnotation := func(annotation string) (string, bool) {
		matches := vars.SpyreCardAnnotationRegex.FindStringSubmatch(annotation)
		if matches == nil {
			return "", false
		}

		return matches[1], true
	}

	for annotationKey, val := range annotations {
		if containerName, ok := isSpyreCardAnnotation(annotationKey); ok {
			valInt, err := strconv.Atoi(val)
			if err != nil {
				return 0, spyreCardContainerMap, fmt.Errorf("failed to convert to int. Provided val: %s is not of int type", val)
			}
			spyreCardContainerMap[containerName] = valInt
			spyreCards += valInt
		}
	}

	return spyreCards, spyreCardContainerMap, nil
}

// JoinPCIAddresses formats a slice of PCI addresses into the space-separated string expected by
// the AIU_PCIE_IDS env var. Addresses discovered on the host may carry trailing whitespace
// (e.g. a newline from `ls`), which is trimmed so it cannot corrupt the separator.
func JoinPCIAddresses(pciList []string) string {
	trimmed := make([]string, 0, len(pciList))
	for _, addr := range pciList {
		if addr = strings.TrimSpace(addr); addr != "" {
			trimmed = append(trimmed, addr)
		}
	}

	return strings.Join(trimmed, " ")
}

// DeletePods deletes all pods and returns any error messages.
func DeletePods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, forceDelete bool) []string {
	var podErrors []string
	for _, pod := range pods {
		if err := rt.DeletePod(ctx, pod.ID, &forceDelete); err != nil {
			// Ignore "not found" errors - pod already deleted or never existed
			if utils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Pod %s already deleted or does not exist", pod.ID)

				continue
			}
			errMsg := fmt.Sprintf("failed to delete pod %s: %s", pod.ID, err)
			podErrors = append(podErrors, errMsg)
		}
	}

	return podErrors
}

// DeleteVolumesFromPods extracts volume names from pod labels and deletes them using the runtime client.
// Volumes are always deleted when keepData=false. This method is only called when keepData=false.
//
// Returns a list of error messages for any volumes that failed to delete.
func DeleteVolumesFromPods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, instanceType string, instanceID uuid.UUID) []string {
	var errorMessages []string
	volumesToDelete := make(map[string]bool) // Use map to avoid duplicates

	// Extract volume names from pod labels
	for _, pod := range pods {
		if volumeNames, ok := pod.Labels[constants.VolumeLabel]; ok && volumeNames != "" {
			// Split comma-separated volume names (in case a pod has multiple volumes)
			volumes := strings.Split(volumeNames, ",")
			for _, volumeName := range volumes {
				volumeName = strings.TrimSpace(volumeName)
				if volumeName != "" {
					volumesToDelete[volumeName] = true
				}
			}
		}
	}

	if len(volumesToDelete) == 0 {
		// Just return if there are no volumes to delete
		return nil
	}

	logger.InfofCtx(ctx, "Deleting %d volume(s) for %s %s", len(volumesToDelete), instanceType, instanceID)

	// Delete each unique volume using the runtime client
	for volumeName := range volumesToDelete {
		if err := rt.DeleteVolume(ctx, volumeName); err != nil {
			// Ignore "not found" errors - volume already deleted or never existed
			if utils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Volume %s already deleted or does not exist", volumeName)

				continue
			}
			errMsg := fmt.Sprintf("%s %s: failed to delete volume %s: %s", instanceType, instanceID, volumeName, err)
			errorMessages = append(errorMessages, errMsg)
			logger.ErrorfCtx(ctx, "%s %s: failed to delete volume %s: %s", instanceType, instanceID, volumeName, err)
		} else {
			logger.InfofCtx(ctx, "Successfully deleted volume: %s", volumeName)
		}
	}

	return errorMessages
}

// DeleteSecretsFromPods extracts secret names from pod labels and deletes them.
//
// Deletion logic:
//   - If keepData=false: Delete ALL secrets (default behavior - complete cleanup)
//   - If keepData=true: Only delete secrets WITHOUT skip-cleanup label OR with skip-cleanup="false" (e.g., API keys)
//     Preserve secrets WITH skip-cleanup="true" (e.g., DB credentials)
//
// Returns a list of error messages for any secrets that failed to delete.
func DeleteSecretsFromPods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, keepData bool, instanceType string, instanceID uuid.UUID) []string {
	secretsToDelete := collectSecretsFromPods(pods, keepData)

	if len(secretsToDelete) == 0 {
		return nil
	}

	logger.InfofCtx(ctx, "Deleting %d secret(s) for %s %s (keepData=%v)", len(secretsToDelete), instanceType, instanceID, keepData)

	return deleteSecrets(ctx, rt, secretsToDelete, instanceType, instanceID)
}

// collectSecretsFromPods extracts the set of secret names to delete from pod labels,
// respecting the keepData flag to preserve secrets marked with the skip-cleanup label.
func collectSecretsFromPods(pods []runtimeTypes.Pod, keepData bool) map[string]bool {
	secretsToDelete := make(map[string]bool)

	for _, pod := range pods {
		secretNames, ok := pod.Labels[catalogconstants.CatalogSecretLabel]
		if !ok {
			continue
		}

		// With keepData enabled, preserve secrets explicitly marked for cleanup skip.
		if skipValue, hasSkipLabel := pod.Labels[catalogconstants.CatalogSecretSkipLabel]; keepData && hasSkipLabel && skipValue != "false" {
			continue
		}

		// Trim entries and use a map so duplicate secret names are deleted once.
		for secretName := range strings.SplitSeq(secretNames, ",") {
			secretName = strings.TrimSpace(secretName)
			if secretName != "" {
				secretsToDelete[secretName] = true
			}
		}
	}

	return secretsToDelete
}

// deleteSecrets deletes each secret in the provided set, ignoring not-found errors.
// Returns a list of error messages for any secrets that failed to delete.
func deleteSecrets(ctx context.Context, rt runtime.Runtime, secretsToDelete map[string]bool, instanceType string, instanceID uuid.UUID) []string {
	var errorMessages []string

	for secretName := range secretsToDelete {
		if err := rt.DeleteSecret(ctx, secretName); err != nil {
			// Ignore "not found" errors - secret already deleted or never existed
			if utils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Secret %s already deleted or does not exist", secretName)

				continue
			}
			errMsg := fmt.Sprintf("%s %s: failed to delete secret %s: %s", instanceType, instanceID, secretName, err)
			errorMessages = append(errorMessages, errMsg)
			logger.ErrorfCtx(ctx, "%s %s: failed to delete secret %s: %s", instanceType, instanceID, secretName, err)
		} else {
			logger.InfofCtx(ctx, "Successfully deleted secret: %s", secretName)
		}
	}

	return errorMessages
}
