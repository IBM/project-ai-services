package deployment

import (
	"slices"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// modelCatalog answers "what is a model?" from the catalog instead of hard-coded type names:
// a component type is a model type when its providers declare `category: models` in their
// metadata.yaml, and the LiteLLM settings come from the provider's `litellm` section.
type modelCatalog struct {
	provider *catalog.CatalogProvider
}

// components returns all model-category components, ordered for deterministic lookups.
func (m modelCatalog) components() []types.Component {
	comps := m.provider.ListComponentsByCategory(catalogconstants.ComponentCategoryModels)
	slices.SortFunc(comps, func(a, b types.Component) int {
		return strings.Compare(a.ComponentType+"/"+a.ID, b.ComponentType+"/"+b.ID)
	})

	return comps
}

// componentTypes returns the distinct, sorted component types that serve models.
func (m modelCatalog) componentTypes() []string {
	var out []string
	for _, c := range m.components() {
		if !slices.Contains(out, c.ComponentType) {
			out = append(out, c.ComponentType)
		}
	}

	return out
}

// validateType rejects a component type that has no model providers in the catalog.
func (m modelCatalog) validateType(componentType string) error {
	valid := m.componentTypes()
	if slices.Contains(valid, componentType) {
		return nil
	}

	return BadRequest("invalid component_type %q; must be one of: %s", componentType, strings.Join(valid, ", "))
}

// modeFor returns the LiteLLM model mode declared by the providers of a component type. It lets
// remote models, which have no component provider of their own, use the same mode as local ones.
//
// It fails with a 400 only when the component type is not a model type. A type whose providers
// declare no mode yields "", which registers the model with the gateway's default mode.
func (m modelCatalog) modeFor(componentType string) (string, error) {
	if err := m.validateType(componentType); err != nil {
		return "", err
	}

	return modeOf(m.components(), componentType), nil
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
