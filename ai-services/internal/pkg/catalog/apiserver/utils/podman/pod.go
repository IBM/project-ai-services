package podman

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
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
// for component-to-service communication. Components do not declare ai-services.io/routes.
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
		return nil, fmt.Errorf("no port found in pod spec for %s", podName)
	}

	endpoint := fmt.Sprintf("%s:%s", podName, targetPort)
	logger.InfofCtx(ctx, "Extracted endpoint for %s: %s (domain: %s, port: %s)\n",
		podName, endpoint, podName, targetPort)

	return &EndpointInfo{
		Endpoint: endpoint,
		Domain:   podName,
		Port:     targetPort,
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
