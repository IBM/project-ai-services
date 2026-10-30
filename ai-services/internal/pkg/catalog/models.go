package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"go.yaml.in/yaml/v3"
)

const modelsFileName = "models.yaml"

// loadModelsFromFile reads models.yaml from the provider's root directory.
func loadModelsFromFile(itemFS fs.FS, providerPath string) ([]types.Model, error) {
	data, err := fs.ReadFile(itemFS, path.Join(providerPath, modelsFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", modelsFileName, err)
	}

	var models []types.Model
	if err := yaml.Unmarshal(data, &models); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", modelsFileName, err)
	}

	return models, nil
}

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
