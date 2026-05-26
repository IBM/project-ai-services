package params

import (
	"context"
	"fmt"
	"maps"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ParamBuilder builds deployment parameters for services.
// It collects parameters from:
// 1. Component-level params from the request
// 2. Flattens component params with component_type prefix (e.g., llm.model)
// 3. Loads service values which merges component values under component_type keys.
type ParamBuilder struct {
	catalogProvider *catalog.CatalogProvider
}

// NewParamBuilder creates a new parameter builder instance.
func NewParamBuilder(
	provider *catalog.CatalogProvider,
) *ParamBuilder {
	return &ParamBuilder{
		catalogProvider: provider,
	}
}

// ComponentParams holds all parameters needed to deploy a component.
type ComponentParams struct {
	ComponentType string            // e.g., "vector_db", "llm", "embedding"
	ProviderID    string            // e.g., "opensearch", "vllm"
	Params        map[string]any    // Component parameters from request
	ArgParams     map[string]string // Flattened params
	Values        map[string]any    // Component values loaded from values.yaml
}

// ServiceParams holds all parameters needed to deploy a service.
type ServiceParams struct {
	CatalogID string            // Service catalog ID (e.g., "chat", "digitize")
	Version   string            // Service version
	ArgParams map[string]string // Flattened component params with component_type prefix (e.g., llm.model)
	Values    map[string]any    // Service values with component values nested under component_type
}

// BuildServiceParams builds parameters for a single service deployment.
func (b *ParamBuilder) BuildServiceParams(
	ctx context.Context,
	svcReq apimodels.Service,
	arch *types.Architecture,
) (*ServiceParams, error) {
	// Load service metadata from catalog
	service, err := b.catalogProvider.LoadService(svcReq.CatalogID)
	if err != nil {
		return nil, fmt.Errorf("failed to load service metadata: %w", err)
	}

	params := &ServiceParams{
		CatalogID: svcReq.CatalogID,
		Version:   svcReq.Version,
		ArgParams: make(map[string]string),
	}

	// Build component params for each component
	componentParams := make(map[string]*ComponentParams)
	for _, compReq := range svcReq.Components {
		compParams, err := b.buildComponentParams(compReq)
		if err != nil {
			return nil, fmt.Errorf("failed to build params for component '%s': %w", compReq.ComponentType, err)
		}

		// Store component params by component type
		componentParams[compReq.ComponentType] = compParams

		// Merge component argParams into service argParams
		maps.Copy(params.ArgParams, compParams.ArgParams)
	}

	// Load service values with component values merged in
	if err := b.loadServiceValues(params, service, componentParams); err != nil {
		return nil, fmt.Errorf("failed to load service values: %w", err)
	}

	return params, nil
}

// buildComponentParams builds parameters for a single component.
// This includes loading component values from values.yaml.
func (b *ParamBuilder) buildComponentParams(
	compReq apimodels.Component,
) (*ComponentParams, error) {
	// Flatten component params with component_type prefix for service-level argParams
	// Example: llm.model = "granite-3.3-8b-instruct" -> accessible as .Values.llm.model
	prefix := compReq.ComponentType
	flatParams := utils.FlattenMapWithValues(compReq.Params, prefix)

	compParams := &ComponentParams{
		ComponentType: compReq.ComponentType,
		ProviderID:    compReq.ProviderID,
		Params:        compReq.Params,
		ArgParams:     flatParams,
	}

	// Load component values from values.yaml with argParams applied
	// Note: We pass flatParams without prefix since LoadComponentValues expects flat keys
	componentArgParams := utils.FlattenMapWithValues(compReq.Params, "")
	values, err := b.catalogProvider.LoadComponentValues(compReq.ComponentType, compReq.ProviderID, componentArgParams)
	if err != nil {
		return nil, fmt.Errorf("failed to load component values: %w", err)
	}

	compParams.Values = values

	return compParams, nil
}

// loadServiceValues loads service values and merges component values under component_type keys.
// This creates the final values structure for template rendering:
// .Values.llm.model, .Values.vector_db.auth.password, etc.
func (b *ParamBuilder) loadServiceValues(
	params *ServiceParams,
	service *types.Service,
	componentParams map[string]*ComponentParams,
) error {
	// Load service's own values.yaml with service-level argParams
	values, err := b.catalogProvider.LoadServiceValues(service.ID, params.ArgParams)
	if err != nil {
		return fmt.Errorf("failed to load service values: %w", err)
	}

	// Merge component values under component_type keys
	// This allows templates to access component values via .Values.<component_type>.<field>
	for componentType, compParams := range componentParams {
		values[componentType] = compParams.Values
	}

	params.Values = values

	return nil
}

// Made with Bob
