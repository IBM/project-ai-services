package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	helmclient "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// yamlDecoderBufSz matches the shared YAML decoder buffer size used for multi-doc manifests.
const yamlDecoderBufSz = 4096

// openShiftSync implements RuntimeSync for the OpenShift runtime.
type openShiftSync struct {
	runtimeClient runtime.Runtime
}

func newOpenShiftSync() *openShiftSync {
	return &openShiftSync{runtimeClient: nil}
}

func (s *openShiftSync) WithRuntime(rt runtime.Runtime) (RuntimeSync, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime client is nil")
	}

	return &openShiftSync{runtimeClient: rt}, nil
}

// FetchPodStatuses fetches all pods labelled with the given templateID using the OpenShift runtime.
func (s *openShiftSync) FetchPodStatuses(ctx context.Context, templateID string) ([]*PodStatus, error) {
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

	podStatuses := make([]*PodStatus, 0, len(filteredPods))
	for _, pod := range filteredPods {
		health := derivePodHealth(pod.Containers)
		podStatuses = append(podStatuses, &PodStatus{
			State:   pod.Status,
			Health:  health,
			PodName: pod.Name,
		})
	}

	return podStatuses, nil
}

// derivePodHealth determines pod-level health from its container statuses.
// A pod is healthy only when all containers report "healthy".
func derivePodHealth(containers []runtimetypes.Container) string {
	if len(containers) == 0 {
		return string(constants.NotReady)
	}

	for _, c := range containers {
		if c.Health != string(constants.Ready) {
			return string(constants.NotReady)
		}
	}

	return string(constants.Ready)
}

// 1. Resolve the Helm release for the service/component instance.
// 2. Read the deployed Helm release manifest.
// 3. Extract the expected resources from that manifest.
// 4. Validate that the deployed resources still exist in the runtime.
// ValidateResources validates OpenShift resources using the deployed Helm release manifest.
func (s *openShiftSync) ValidateResources(ctx context.Context, input ResourceValidationInput) string {
	if s.runtimeClient == nil {
		return "runtime client not configured"
	}

	// Use the deployed release manifest as the source of truth for expected resources.
	manifest, err := s.getReleaseManifest(input.AppID, input.CatalogID, input.ItemType)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to get Helm release manifest for %s %s: %v", input.ItemType, input.CatalogID, err)

		return ""
	}

	// Count workload, secret, and PVC resources from the stored release manifest.
	expectedCounts, err := s.countResourcesFromManifest(manifest)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to parse Helm release manifest for %s %s: %v", input.ItemType, input.CatalogID, err)

		return ""
	}

	var errorMessages []string

	// OpenShift workloads may be manually scaled, so require at least one pod when a workload exists.
	if expectedCounts.Pods > 0 && input.ActualPodCount == 0 {
		errorMessages = append(errorMessages, "No pods found for deployed workload")
	}

	// Reuse shared existence checks for secrets and PVC-backed volumes.
	if resourceValidationMsg := validateResourceExistenceChecks(ctx, expectedCounts.SecretNames, expectedCounts.VolumeNames, s.runtimeClient); resourceValidationMsg != "" {
		errorMessages = append(errorMessages, resourceValidationMsg)
	}

	return strings.Join(errorMessages, "; ")
}

// countResourcesFromManifest walks each YAML document in the Helm release manifest.
func (s *openShiftSync) countResourcesFromManifest(manifest string) (*ResourceCounts, error) {
	counts := &ResourceCounts{}
	// Helm stores resources as a multi-document YAML manifest.
	decoder := apiyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), yamlDecoderBufSz)

	for {
		var resource unstructured.Unstructured
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}
		// Skip empty YAML documents between --- separators.
		if resource.GetKind() == "" {
			continue
		}

		s.countManifestResource(&resource, counts)
	}

	return counts, nil
}

// countManifestResource records only resource kinds relevant to sync validation.
func (s *openShiftSync) countManifestResource(resource *unstructured.Unstructured, counts *ResourceCounts) {
	switch resource.GetKind() {
	case "Deployment", "DeploymentConfig", "StatefulSet":
		// These controller kinds represent workload instances we expect to be present.
		counts.Pods++
		s.extractManifestPodResources(resource, counts)
	case "Secret":
		counts.SecretNames = append(counts.SecretNames, resource.GetName())
	case "PersistentVolumeClaim":
		counts.VolumeNames = append(counts.VolumeNames, resource.GetName())
	}
}

// extractManifestPodResources reads secret and PVC references from a pod template.
func (s *openShiftSync) extractManifestPodResources(resource *unstructured.Unstructured, counts *ResourceCounts) {
	// Workload templates keep mounted resource references under spec.template.spec.volumes.
	volumes, found, err := unstructured.NestedSlice(resource.Object, "spec", "template", "spec", "volumes")
	if err != nil || !found {
		return
	}

	for _, volume := range volumes {
		volumeMap, ok := volume.(map[string]any)
		if !ok {
			continue
		}

		if name := manifestSecretVolumeName(volumeMap); name != "" {
			counts.SecretNames = append(counts.SecretNames, name)
		}

		if name := manifestPVCVolumeName(volumeMap); name != "" {
			counts.VolumeNames = append(counts.VolumeNames, name)
		}
	}
}

func manifestSecretVolumeName(volumeMap map[string]any) string {
	// Secret volumes depend on the referenced secret continuing to exist.
	secret, ok := volumeMap["secret"].(map[string]any)
	if !ok {
		return ""
	}

	name, ok := secret["secretName"].(string)
	if !ok {
		return ""
	}

	return name
}

func manifestPVCVolumeName(volumeMap map[string]any) string {
	// PVC volumes depend on the referenced claim continuing to exist.
	pvc, ok := volumeMap["persistentVolumeClaim"].(map[string]any)
	if !ok {
		return ""
	}

	name, ok := pvc["claimName"].(string)
	if !ok {
		return ""
	}

	return name
}

// getReleaseManifest resolves the namespace and release name, then fetches the deployed manifest.
func (s *openShiftSync) getReleaseManifest(appID, catalogID, itemType string) (string, error) {
	appUUID, err := uuid.Parse(appID)
	if err != nil {
		return "", fmt.Errorf("invalid application ID: %w", err)
	}

	// Releases are installed into the application namespace derived from the app ID.
	namespace := catalogutils.AppNamespace(appUUID)
	releaseName, err := s.releaseName(appUUID, catalogID, itemType)
	if err != nil {
		return "", err
	}

	helm, err := helmclient.NewHelm(namespace)
	if err != nil {
		return "", fmt.Errorf("failed to create helm client: %w", err)
	}

	manifest, err := helm.GetReleaseManifest(releaseName)
	if err != nil {
		return "", err
	}

	return manifest, nil
}

// releaseName mirrors the naming used by the OpenShift deployer for services and components.
func (s *openShiftSync) releaseName(appID uuid.UUID, catalogID, itemType string) (string, error) {
	switch itemType {
	case resourceItemTypeService:
		return catalogutils.HelmReleaseName(appID, catalogID), nil
	case resourceItemTypeComponent:
		parts := strings.Split(catalogID, "/")
		if len(parts) != componentCatalogIDParts {
			return "", fmt.Errorf("invalid component catalogID format: %s (expected format: type/provider)", catalogID)
		}

		return catalogutils.HelmReleaseName(appID, strings.ReplaceAll(parts[0], "_", "-")), nil
	default:
		return "", fmt.Errorf("unknown item type: %s", itemType)
	}
}

// Ensure openShiftSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*openShiftSync)(nil)
