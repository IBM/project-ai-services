package podman

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
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
	info, err := ExtractPodEndpoint(ctx, podSpec)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to extract component endpoint info: %v\n", err)

		return
	}
	if info == nil {
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

// BaseDirProvider represents any runtime capable of reporting its base directory on the target host
// (e.g. RemoteRuntime querying over gRPC, or a MockRuntime in tests).
type BaseDirProvider interface {
	GetBaseDir(ctx context.Context) (string, error)
}

// FetchBaseDir resolves the base directory used for deployment artifacts on the target host.
// For runtimes implementing BaseDirProvider (such as RemoteRuntime) it is queried dynamically;
// otherwise the control-plane base directory is used as a fallback.
func FetchBaseDir(ctx context.Context, rt runtime.Runtime) (string, error) {
	if provider, ok := rt.(BaseDirProvider); ok {
		baseDir, err := provider.GetBaseDir(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to resolve base directory: %w", err)
		}
		if strings.TrimSpace(baseDir) == "" {
			return "", fmt.Errorf("runtime returned empty base directory")
		}

		return baseDir, nil
	}

	return utils.GetBaseDir(), nil
}

// SpyreDiscoverer represents any runtime capable of discovering free Spyre cards
// (e.g. RemoteRuntime querying over gRPC, or a MockRuntime in tests).
type SpyreDiscoverer interface {
	FindFreeSpyreCards(ctx context.Context) ([]string, error)
}

// NewSpyreCardPool discovers the free Spyre cards on the target host and returns a pool of them.
// Discovery is delegated to the runtime when it implements SpyreDiscoverer (e.g. RemoteRuntime
// querying over gRPC, or a MockRuntime in tests), otherwise falling back to local host discovery.
// Callers must only call this when required > 0.
func NewSpyreCardPool(ctx context.Context, rt runtime.Runtime, required int) (*deploymenttypes.SpyreCardPool, error) {
	var (
		pciAddresses []string
		err          error
	)

	if discoverer, ok := rt.(SpyreDiscoverer); ok {
		pciAddresses, err = discoverer.FindFreeSpyreCards(ctx)
	} else {
		pciAddresses, err = helpers.FindFreeSpyreCards(ctx)
	}
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

// DeleteComponents deletes the given components (pods, secrets, volumes and DB records) and returns
// any error messages. Shared by the application deletion and the model deletion flows.
//
//nolint:cyclop // Function complexity is acceptable for deletion orchestration
func DeleteComponents(ctx context.Context, rt runtime.Runtime, componentRepo dbrepo.ComponentRepository, componentIDs []uuid.UUID, keepData bool) []string {
	var errorMessages []string
	forceDelete := true

	for _, componentID := range componentIDs {
		// List component pods
		pods, err := rt.ListPods(ctx, map[string][]string{
			"label": {fmt.Sprintf("ai-services.io/template=%s", componentID)},
		})
		if err != nil {
			errMsg := fmt.Sprintf("component %s: failed to list pods: %s", componentID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to list pods: %s", err))

			continue
		}

		// Delete component secrets
		secretErrors := DeleteSecretsFromPods(ctx, rt, pods, keepData, "component", componentID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete component pods first so runtime releases attached volumes.
		podErrors := DeletePods(ctx, rt, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("component %s: failed to delete %d pod(s)", componentID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete component volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := DeleteVolumesFromPods(ctx, rt, pods, "component", componentID)
			if len(volumeErrors) > 0 {
				hasDeletionErrors = true
				errorMessages = append(errorMessages, volumeErrors...)
			}
		}

		if hasDeletionErrors {
			continue
		}

		// Delete component from DB
		if err := componentRepo.Delete(ctx, componentID); err != nil {
			errMsg := fmt.Sprintf("component %s: failed to delete from DB: %s", componentID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to delete from DB: %s", err))
		}
	}

	return errorMessages
}
