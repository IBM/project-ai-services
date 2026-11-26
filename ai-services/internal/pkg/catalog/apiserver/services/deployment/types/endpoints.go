package types

import (
	"context"
	"maps"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// MergeEndpoints merges the endpoints of comp into the values of every service that uses it, so
// the service's templates can reach it as .Values.<componentType>.host and .port. It is plan
// bookkeeping that does not depend on the runtime, and it is what makes a component deployed by
// any runtime, or a model started by the model service, reachable from the services.
//
// It is safe to call from the goroutines that deploy the components concurrently.
func (p *DeploymentPlan) MergeEndpoints(ctx context.Context, comp *ComponentPlan) {
	p.valuesMu.Lock()
	defer p.valuesMu.Unlock()

	if len(comp.Endpoints) == 0 {
		logger.InfofCtx(ctx, "Component %s has no endpoints to merge\n", comp.ComponentType)

		return
	}

	for _, serviceID := range comp.UsedByServices {
		p.mergeEndpointsIntoService(ctx, comp, serviceID)
	}
}

// MergeComponentValues merges values into the values of componentType in the service serviceID, so
// the service's templates read them as .Values.<componentType>.<key>. A service that is not in the
// plan is ignored. It is safe to call from the goroutines that deploy the components concurrently.
func (p *DeploymentPlan) MergeComponentValues(ctx context.Context, serviceID, componentType string, values map[string]any) {
	p.valuesMu.Lock()
	defer p.valuesMu.Unlock()

	svc, ok := p.Services[serviceID]
	if !ok {
		return
	}

	if svc.Values == nil {
		svc.Values = make(map[string]any)
	}

	mergeServiceValues(ctx, svc, componentType, values, serviceID)
}

// mergeEndpointsIntoService merges the endpoint data of comp into one service, including the
// component's instanceSlug so templates can reference it as .Values.<type>.instanceSlug.
func (p *DeploymentPlan) mergeEndpointsIntoService(ctx context.Context, comp *ComponentPlan, serviceID string) {
	svc, ok := p.Services[serviceID]
	if !ok {
		return
	}

	if svc.Values == nil {
		svc.Values = make(map[string]any)
	}

	endpointData, ok := comp.Endpoints[comp.ComponentType]
	if !ok {
		logger.ErrorfCtx(ctx, "Component %s endpoint data not found in comp.Endpoints map\n", comp.ComponentType)

		return
	}

	// Inject instanceSlug into the endpoint map before merging, so it is always present
	// regardless of whether the service already has values for this component type.
	// mergeServiceValues clones the map when creating a fresh entry, so this write is safe.
	if endpointMap, ok := endpointData.(map[string]any); ok {
		instanceSlug := catalogutils.GenerateInstanceSlug(comp.DatabaseID.String())
		endpointMap["instanceSlug"] = instanceSlug
		logger.InfofCtx(ctx, "Added instanceSlug '%s' to component %s in service %s\n", instanceSlug, comp.ComponentType, serviceID)
	}

	mergeServiceValues(ctx, svc, comp.ComponentType, endpointData, serviceID)
}

// mergeServiceValues sets endpointData as the service's values of componentType, or merges it into
// them when both are maps.
func mergeServiceValues(ctx context.Context, svc *ServicePlan, componentType string, endpointData any, serviceID string) {
	endpointMap, isEndpointMap := endpointData.(map[string]any)

	existingMap, isMap := svc.Values[componentType].(map[string]any)
	if !isMap {
		// Copy, so that later merges into the service's values do not write into the component's.
		if isEndpointMap {
			endpointData = maps.Clone(endpointMap)
		}

		svc.Values[componentType] = endpointData

		return
	}

	if isEndpointMap {
		maps.Copy(existingMap, endpointMap)
		logger.InfofCtx(ctx, "Updated component %s host/port in service %s\n", componentType, serviceID)
	}
}
