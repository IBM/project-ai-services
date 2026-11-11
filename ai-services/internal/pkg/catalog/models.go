package catalog

import (
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

const modelsFileName = "models.yaml"

// injectModelsIntoSchema sets properties.model.oneOf and properties.model.default
// on the decoded values.schema.json from the given models. It is a no-op when
// there are no models or the schema has no "model" property.
func injectModelsIntoSchema(schema map[string]any, models []types.Model) {
	if len(models) == 0 {
		return
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	modelField, ok := props["model"].(map[string]any)
	if !ok {
		return
	}

	oneOf := make([]any, 0, len(models))
	defaultID := ""
	for _, m := range models {
		oneOf = append(oneOf, map[string]any{
			"const":       m.ID,
			"title":       m.Title,
			"description": m.Description,
		})
		if m.Default && defaultID == "" {
			defaultID = m.ID
		}
	}
	if defaultID == "" {
		defaultID = models[0].ID
	}

	modelField["oneOf"] = oneOf
	modelField["default"] = defaultID
}
