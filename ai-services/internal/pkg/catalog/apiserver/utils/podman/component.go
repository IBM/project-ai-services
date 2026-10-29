package podman

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ComponentPodsParams holds everything required to deploy the pods of a single catalog component.
// It is shared by the application deployment path and the model deployment path.
type ComponentPodsParams struct {
	Runtime       runtime.Runtime
	ComponentID   uuid.UUID
	ComponentType string
	// Values are the rendered catalog values (defaults from values.yaml + overrides).
	Values map[string]any
	// Metadata is the component runtime metadata; PodTemplateExecutions defines the deploy order.
	Metadata  *templates.AppMetadata
	Templates map[string]*template.Template
	// SpyrePool must be populated when any of the templates declares spyre-card annotations.
	SpyrePool *deploymenttypes.SpyreCardPool
	BaseDir   string
}

// DeployComponentPods renders and deploys every pod template of a component, following
// PodTemplateExecutions when defined (otherwise all templates). It returns the extracted
// endpoints keyed by component type: {"<componentType>": {"host": <podName>, "port": <port>}}.
func DeployComponentPods(ctx context.Context, p ComponentPodsParams) (map[string]any, error) {
	endpoints := make(map[string]any)

	if p.Metadata != nil && len(p.Metadata.PodTemplateExecutions) > 0 {
		for _, layer := range p.Metadata.PodTemplateExecutions {
			for _, templateName := range layer {
				if err := deployComponentTemplate(ctx, p, templateName, endpoints); err != nil {
					return nil, fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
				}
			}
		}

		return endpoints, nil
	}

	logger.InfofCtx(ctx, "No PodTemplateExecutions defined for component %s, deploying all templates\n", p.ComponentType)
	for templateName := range p.Templates {
		if err := deployComponentTemplate(ctx, p, templateName, endpoints); err != nil {
			return nil, fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
		}
	}

	return endpoints, nil
}

// deployComponentTemplate deploys a single component pod template and records its endpoint.
func deployComponentTemplate(ctx context.Context, p ComponentPodsParams, templateName string, endpoints map[string]any) error {
	logger.InfofCtx(ctx, "Deploying component template '%s'...\n", templateName)

	podTemplate, ok := p.Templates[templateName]
	if !ok {
		return fmt.Errorf("pod template '%s' not found", templateName)
	}

	initialParams := map[string]any{
		"InstanceSlug": catalogutils.GenerateInstanceSlug(p.ComponentID.String()),
		"TemplateID":   p.ComponentID,
		"BaseDir":      p.BaseDir,
		"Values":       p.Values,
		"env":          map[string]map[string]string{},
	}

	podSpec, err := RenderAndParsePodTemplate(podTemplate, templateName, initialParams)
	if err != nil {
		return err
	}

	env, err := GetEnvParams(ctx, podSpec, p.SpyrePool)
	if err != nil {
		return fmt.Errorf("failed to get env params: %w", err)
	}

	finalPodSpec, renderedBytes, err := RenderFinalPodTemplate(podTemplate, templateName, initialParams, env)
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(renderedBytes)) == "" {
		// skip deploy if there is nothing to apply
		return nil
	}

	exists, err := p.Runtime.PodExists(ctx, finalPodSpec.Name)
	if err != nil {
		return fmt.Errorf("failed to check pod existence: %w", err)
	}
	if exists {
		logger.InfofCtx(ctx, "Pod '%s' already exists, skipping deployment\n", finalPodSpec.Name)

		return nil
	}

	if err := DeployPodmanPod(ctx, p.Runtime, finalPodSpec, renderedBytes, templateName); err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Component template '%s' deployed successfully\n", templateName)

	recordComponentEndpoint(ctx, endpoints, p.ComponentType, finalPodSpec)

	return nil
}

// recordComponentEndpoint stores the pod host and port under the component type key.
func recordComponentEndpoint(ctx context.Context, endpoints map[string]any, componentType string, podSpec *podmodels.PodSpec) {
	info, err := ExtractPodEndpointInfo(ctx, podSpec)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to extract component endpoint info: %v\n", err)

		return
	}

	endpoints[componentType] = map[string]any{
		"host": info.Domain,
		"port": info.Port,
	}
	logger.InfofCtx(ctx, "Component '%s' endpoint: %s:%s\n", componentType, info.Domain, info.Port)
}

// GetEnvParams returns the per-container environment parameters for a pod, including the
// Spyre card PCI addresses allocated from pool for containers that declare spyre-card annotations.
func GetEnvParams(ctx context.Context, podSpec *podmodels.PodSpec, pool *deploymenttypes.SpyreCardPool) (map[string]map[string]string, error) {
	env := make(map[string]map[string]string)

	for _, container := range podSpec.Spec.Containers {
		env[container.Name] = make(map[string]string)
	}

	spyreCards, spyreCardContainerMap, err := FetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
	if err != nil {
		return env, err
	}

	if spyreCards == 0 {
		return env, nil
	}

	if pool == nil {
		return env, fmt.Errorf("spyre cards required but pool was not populated")
	}

	for containerName, spyreCount := range spyreCardContainerMap {
		if spyreCount == 0 {
			continue
		}

		allocatedAddresses, err := pool.Allocate(spyreCount)
		if err != nil {
			return env, fmt.Errorf("failed to allocate Spyre cards for container %s: %w", containerName, err)
		}

		pciAddressStr := JoinPCIAddresses(allocatedAddresses)
		env[containerName][string(constants.PCIAddressKey)] = pciAddressStr

		logger.DebugfCtx(ctx, "Allocated %d Spyre cards to container '%s' in pod '%s': %s\n",
			spyreCount, containerName, podSpec.Name, pciAddressStr)
	}

	return env, nil
}

// FetchBaseDir resolves the base directory used for deployment artifacts on the target host.
// For remote workers it is queried over gRPC (the worker's own AI_SERVICES_BASE_DIR); for a
// local runtime the control-plane value is used.
func FetchBaseDir(ctx context.Context, rt runtime.Runtime) (string, error) {
	if remoteRT, ok := rt.(*remoteruntime.RemoteRuntime); ok {
		baseDir, err := remoteRT.GetBaseDir(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to resolve base directory on worker %q: %w", remoteRT.WorkerName(), err)
		}
		if strings.TrimSpace(baseDir) == "" {
			return "", fmt.Errorf("worker %q returned empty base directory", remoteRT.WorkerName())
		}

		return baseDir, nil
	}

	return utils.GetBaseDir(), nil
}

// NewSpyreCardPool discovers the free Spyre cards on the target host and returns a pool of them.
// Discovery is sent over the gRPC CommandStream, so rt must be a RemoteRuntime (this covers both
// the local worker and remote workers). Callers must only call this when required > 0.
func NewSpyreCardPool(ctx context.Context, rt runtime.Runtime, required int) (*deploymenttypes.SpyreCardPool, error) {
	remoteRT, ok := rt.(*remoteruntime.RemoteRuntime)
	if !ok {
		return nil, fmt.Errorf("runtime does not support Spyre card discovery (type %T)", rt)
	}

	pciAddresses, err := remoteRT.FindFreeSpyreCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find free Spyre cards: %w", err)
	}

	available := len(pciAddresses)
	logger.InfofCtx(ctx, "Available Spyre cards: %d, required: %d\n", available, required)

	if available < required {
		return nil, fmt.Errorf("insufficient Spyre cards: required %d, available %d", required, available)
	}

	return &deploymenttypes.SpyreCardPool{Addresses: pciAddresses}, nil
}
