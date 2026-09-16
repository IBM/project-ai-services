package sync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	texttemplate "text/template"

	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	modelpkg "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
)

// podmanSync implements RuntimeSync for the Podman runtime.
type podmanSync struct {
	catalogProvider *catalogpkg.CatalogProvider
	runtimeType     string
	runtimeClient   runtime.Runtime
	resourceCache   map[string]*ResourceCounts
	cacheMutex      *sync.RWMutex
}

func newPodmanSync(catalogProvider *catalogpkg.CatalogProvider, runtimeType string) (*podmanSync, error) {
	scopedProvider, err := catalogProvider.WithRuntime(runtimeType)
	if err != nil {
		return nil, fmt.Errorf("failed to scope catalog provider for runtime %q: %w", runtimeType, err)
	}

	return &podmanSync{
		catalogProvider: scopedProvider,
		runtimeType:     runtimeType,
		runtimeClient:   nil,
		resourceCache:   make(map[string]*ResourceCounts),
		cacheMutex:      &sync.RWMutex{},
	}, nil
}

func (s *podmanSync) WithRuntime(rt runtime.Runtime) (RuntimeSync, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime client is nil")
	}

	return &podmanSync{
		catalogProvider: s.catalogProvider,
		runtimeType:     s.runtimeType,
		runtimeClient:   rt,
		resourceCache:   s.resourceCache,
		cacheMutex:      s.cacheMutex,
	}, nil
}

// FetchPodStatuses fetches all pods labelled with the given templateID and returns their statuses.
// It uses InspectPod and InspectContainer (via common.ProcessPod) to derive state and health.
func (s *podmanSync) FetchPodStatuses(ctx context.Context, templateID string) ([]*PodStatus, error) {
	if s.runtimeClient == nil {
		return nil, fmt.Errorf("runtime client not configured")
	}

	filteredPods, err := common.FetchFilteredPods(ctx, s.runtimeClient, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	var podStatuses []*PodStatus
	for _, pod := range filteredPods {
		processedPod, err := common.ProcessPod(ctx, s.runtimeClient, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}

		if processedPod == nil {
			return nil, fmt.Errorf("pod processing returned nil")
		}

		podStatuses = append(podStatuses, &PodStatus{
			State:   processedPod.State,
			Health:  processedPod.Health,
			PodName: processedPod.Name,
		})
	}

	return podStatuses, nil
}

// ValidateResources validates runtime resources using expected counts derived from catalog templates.
// runtimeType is the resolved runtime for this application and is used to select the correct
// catalog subdirectory (e.g. "podman/templates") rather than any server-global default.
func (s *podmanSync) ValidateResources(ctx context.Context, input ResourceValidationInput) string {
	if s.runtimeClient == nil {
		return "runtime client not configured"
	}

	expectedCounts := s.getOrCountTemplateResources(ctx, input.CatalogID, input.InstanceID, input.ItemType)
	if expectedCounts == nil {
		return ""
	}

	var errorMessages []string

	if input.ActualPodCount < expectedCounts.Pods {
		errorMessages = append(errorMessages,
			fmt.Sprintf("Pod count mismatch: expected %d, found %d", expectedCounts.Pods, input.ActualPodCount))
	}

	if resourceValidationMsg := validateResourceExistenceChecks(ctx, expectedCounts.SecretNames, expectedCounts.VolumeNames, s.runtimeClient); resourceValidationMsg != "" {
		errorMessages = append(errorMessages, resourceValidationMsg)
	}

	return strings.Join(errorMessages, "; ")
}

// validateResourceExistenceChecks validates that the expected secrets and volumes exist.
func validateResourceExistenceChecks(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	if len(expectedSecretNames) == 0 && len(expectedVolumeNames) == 0 {
		return ""
	}

	var errorMessages []string

	if msg := validateResourceExistence(ctx, expectedSecretNames, "secret", rt.SecretExists); msg != "" {
		errorMessages = append(errorMessages, msg)
	}

	if msg := validateResourceExistence(ctx, expectedVolumeNames, "volume", rt.VolumeExists); msg != "" {
		errorMessages = append(errorMessages, msg)
	}

	return strings.Join(errorMessages, "; ")
}

// validateResourceExistence is a generic helper to validate resource existence via the provided lookup func.
func validateResourceExistence(ctx context.Context, resourceNames []string, resourceType string, existsFunc func(context.Context, string) (bool, error)) string {
	var missing []string

	for _, name := range resourceNames {
		exists, err := existsFunc(ctx, name)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to check %s existence for %s: %v", resourceType, name, err)

			continue
		}

		if !exists {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return ""
	}

	return fmt.Sprintf("missing %s(s): %s", resourceType, strings.Join(missing, ", "))
}

// getExpectedResourceCounts retrieves expected resource counts from cache.
func (s *podmanSync) getExpectedResourceCounts(instanceID string) *ResourceCounts {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	return s.resourceCache[instanceID]
}

// setExpectedResourceCounts stores expected resource counts in cache.
func (s *podmanSync) setExpectedResourceCounts(instanceID string, counts *ResourceCounts) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	s.resourceCache[instanceID] = counts
}

// countResourcesFromTemplates counts expected Pods and Secrets from service/component templates.
func (s *podmanSync) countResourcesFromTemplates(ctx context.Context, catalogID, instanceID, itemType string) (*ResourceCounts, error) {
	if s.catalogProvider == nil {
		return nil, fmt.Errorf("catalog provider not initialized")
	}

	templates, values, err := s.loadTemplatesAndValues(catalogID, instanceID, itemType)
	if err != nil {
		return nil, err
	}

	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	counts := s.processTemplatesForResourceCounts(ctx, templates, values, instanceSlug)
	if counts == nil {
		return nil, fmt.Errorf("failed to process templates")
	}

	logger.InfofCtx(ctx, "Counted resources for %s %s: %d pods, %d secret refs, %d volume refs",
		itemType, catalogID, counts.Pods, len(counts.SecretNames), len(counts.VolumeNames))

	return counts, nil
}

// loadTemplatesAndValues loads templates and values based on item type.
func (s *podmanSync) loadTemplatesAndValues(catalogID, instanceID, itemType string) (map[string]*texttemplate.Template, map[string]any, error) {
	switch itemType {
	case resourceItemTypeService:
		return s.loadServiceTemplatesAndValues(catalogID, instanceID)
	case resourceItemTypeComponent:
		return s.loadComponentTemplatesAndValues(catalogID, instanceID)
	default:
		return nil, nil, fmt.Errorf("unknown item type: %s", itemType)
	}
}

// loadServiceTemplatesAndValues loads service templates and values.
func (s *podmanSync) loadServiceTemplatesAndValues(catalogID, instanceID string) (map[string]*texttemplate.Template, map[string]any, error) {
	templates, err := s.catalogProvider.LoadServiceTemplates(catalogID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load service templates: %w", err)
	}

	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	overrides := map[string]string{
		"InstanceSlug": instanceSlug,
		"TemplateID":   instanceID,
	}

	values, err := s.catalogProvider.LoadServiceValues(catalogID, overrides)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load service values: %w", err)
	}

	return templates, values, nil
}

// loadComponentTemplatesAndValues loads component templates and values.
func (s *podmanSync) loadComponentTemplatesAndValues(catalogID, instanceID string) (map[string]*texttemplate.Template, map[string]any, error) {
	parts := strings.Split(catalogID, "/")
	if len(parts) != componentCatalogIDParts {
		return nil, nil, fmt.Errorf("invalid component catalogID format: %s (expected format: type/provider)", catalogID)
	}
	componentType, providerID := parts[0], parts[1]

	templates, err := s.catalogProvider.LoadComponentTemplates(componentType, providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	overrides := map[string]string{
		"InstanceSlug": instanceSlug,
		"TemplateID":   instanceID,
	}

	values, err := s.catalogProvider.LoadComponentValues(componentType, providerID, overrides)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load component values: %w", err)
	}

	return templates, values, nil
}

// processTemplatesForResourceCounts processes templates and counts resources.
func (s *podmanSync) processTemplatesForResourceCounts(ctx context.Context, templates map[string]*texttemplate.Template, values map[string]any, instanceSlug string) *ResourceCounts {
	counts := &ResourceCounts{}

	processor := func(templateName string, podSpec *modelpkg.PodSpec) error {
		if podSpec.Kind != "Pod" {
			return nil
		}

		counts.Pods++
		s.extractResourceLabelsFromPodSpec(podSpec, counts)

		return nil
	}

	if err := s.catalogProvider.ProcessTemplates(ctx, templates, values, instanceSlug, processor); err != nil {
		logger.ErrorfCtx(ctx, "Failed to process templates: %v", err)

		return nil
	}

	return counts
}

// extractResourceLabelsFromPodSpec extracts secret and volume labels from pod spec.
func (s *podmanSync) extractResourceLabelsFromPodSpec(podSpec *modelpkg.PodSpec, counts *ResourceCounts) {
	if podSpec.Labels == nil {
		return
	}

	if secretLabel, ok := podSpec.Labels["ai-services.io/secret"]; ok && secretLabel != "" {
		for name := range strings.SplitSeq(secretLabel, ",") {
			if name = strings.TrimSpace(name); name != "" {
				counts.SecretNames = append(counts.SecretNames, name)
			}
		}
	}

	if volumeLabel, ok := podSpec.Labels["ai-services.io/volume"]; ok && volumeLabel != "" {
		for name := range strings.SplitSeq(volumeLabel, ",") {
			if name = strings.TrimSpace(name); name != "" {
				counts.VolumeNames = append(counts.VolumeNames, name)
			}
		}
	}
}

func (s *podmanSync) getOrCountTemplateResources(ctx context.Context, catalogID, instanceID, itemType string) *ResourceCounts {
	cacheKey := catalogID + ":" + instanceID
	expectedCounts := s.getExpectedResourceCounts(cacheKey)
	if expectedCounts != nil {
		return expectedCounts
	}

	counts, err := s.countResourcesFromTemplates(ctx, catalogID, instanceID, itemType)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to count resources from templates for %s %s (runtime: %s): %v", itemType, catalogID, s.runtimeType, err)

		return nil
	}

	s.setExpectedResourceCounts(cacheKey, counts)

	return counts
}

// Ensure podmanSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*podmanSync)(nil)
