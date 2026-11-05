package helpers

import (
	"slices"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// ModelCatalog answers "what is a model?" from the catalog instead of hard-coded type names:
// a component type is a model type when its providers declare `category: models` in their
// metadata.yaml, and the LiteLLM settings come from the provider's `litellm` section.
type ModelCatalog struct {
	provider *catalog.CatalogProvider
}

// New creates a ModelCatalog backed by the given catalog provider.
func New(provider *catalog.CatalogProvider) ModelCatalog {
	return ModelCatalog{provider: provider}
}

// Components returns all model-category components, ordered for deterministic lookups.
func (m ModelCatalog) Components() []types.Component {
	comps := m.provider.ListComponentsByCategory(catalogconstants.ComponentCategoryModels)
	slices.SortFunc(comps, func(a, b types.Component) int {
		return strings.Compare(a.ComponentType+"/"+a.ID, b.ComponentType+"/"+b.ID)
	})

	return comps
}

// ComponentTypes returns the distinct, sorted component types that serve models.
func (m ModelCatalog) ComponentTypes() []string {
	var out []string
	for _, c := range m.Components() {
		if !slices.Contains(out, c.ComponentType) {
			out = append(out, c.ComponentType)
		}
	}

	return out
}

// ValidateType rejects a component type that has no model providers in the catalog.
func (m ModelCatalog) ValidateType(componentType string) error {
	valid := m.ComponentTypes()
	if slices.Contains(valid, componentType) {
		return nil
	}

	return validators.BadRequest("invalid component_type %q; must be one of: %s", componentType, strings.Join(valid, ", "))
}

// ModeFor returns the LiteLLM model mode declared by the providers of a component type. It lets
// remote models, which have no component provider of their own, use the same mode as local ones.
//
// It fails with a 400 only when the component type is not a model type. A type whose providers
// declare no mode yields "", which registers the model with the gateway's default mode.
func (m ModelCatalog) ModeFor(componentType string) (string, error) {
	if err := m.ValidateType(componentType); err != nil {
		return "", err
	}

	return modeOf(m.Components(), componentType), nil
}

// modeOf returns the first non-empty LiteLLM model mode declared by a provider of componentType.
func modeOf(comps []types.Component, componentType string) string {
	for _, c := range comps {
		if c.ComponentType == componentType && c.LiteLLM != nil && c.LiteLLM.ModelMode != "" {
			return c.LiteLLM.ModelMode
		}
	}

	return ""
}
