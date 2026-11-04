package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"go.yaml.in/yaml/v3"
)

const (
	modelsFileName   = "models.yaml"
	profilesFileName = "profiles.yaml"
)

// loadYAMLList reads dir/name as a YAML list. A missing file is not an error (nil, nil).
func loadYAMLList[T any](fsys fs.FS, dir, name string) ([]T, error) {
	data, err := fs.ReadFile(fsys, path.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", name, err)
	}

	var items []T
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", name, err)
	}

	return items, nil
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
