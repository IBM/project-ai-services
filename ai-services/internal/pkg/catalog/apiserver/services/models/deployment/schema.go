package deployment

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// validateLocalParams checks that the provider is a model provider in the catalog and that params
// are valid against its values.schema.json. It returns the provider's component metadata.
func validateLocalParams(ctx context.Context, provider *catalog.CatalogProvider, req apimodels.CreateModelRequest) (*types.Component, error) {
	comp, err := provider.LoadComponent(req.ComponentType, req.ProviderID)
	if err != nil {
		return nil, NotFound("provider %q not found for component type %q", req.ProviderID, req.ComponentType)
	}

	if comp.Category != catalogconstants.ComponentCategoryModels || comp.LiteLLM == nil {
		return nil, BadRequest("provider %q of component type %q does not serve models", req.ProviderID, req.ComponentType)
	}

	schema, err := provider.GetComponentProviderParams(ctx, req.ComponentType, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load param schema for %s/%s: %w", req.ComponentType, req.ProviderID, err)
	}

	if err := validators.ValidateParams(req.Params, schema, fmt.Sprintf("provider %q", req.ProviderID)); err != nil {
		return nil, err
	}

	return comp, nil
}

// validateRemoteParams checks that the model connector provider exists and that params satisfy its
// schema.json. It returns the set of sensitive (format: password) param names, which must be passed
// to the gateway but never persisted.
func validateRemoteParams(ctx context.Context, provider *catalog.CatalogProvider, req apimodels.CreateModelRequest) (map[string]bool, error) {
	if !provider.ConnectorExists(catalogconstants.ConnectorTypeModels, req.ProviderID) {
		return nil, NotFound("provider %q not found for remote models", req.ProviderID)
	}

	raw, err := provider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeModels, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load param schema for provider %q: %w", req.ProviderID, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode param schema for provider %q: %w", req.ProviderID, err)
	}

	// ValidateParams skips empty params, so required fields would go unchecked without this guard.
	if len(schema) > 0 && len(req.Params) == 0 {
		return nil, BadRequest("params is required for provider %q", req.ProviderID)
	}

	if err := validators.ValidateParams(req.Params, schema, fmt.Sprintf("provider %q", req.ProviderID)); err != nil {
		return nil, err
	}

	return catalogutils.SensitiveFieldsFromSchema(schema), nil
}
