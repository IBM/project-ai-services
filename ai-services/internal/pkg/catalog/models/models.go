// Package models reads and processes model definitions (models.yaml) from a provider's asset directory.
package models

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/profiles"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// List returns the models in dir/models.yaml. A missing file means no models (nil, nil).
func List(fsys fs.FS, dir string) ([]types.Model, error) {
	return utils.LoadYAMLList[types.Model](fsys, dir, constants.ModelsFileName)
}

// Find returns modelID's entry from dir/models.yaml, or nil if it isn't listed.
func Find(fsys fs.FS, dir, modelID string) (*types.Model, error) {
	list, err := List(fsys, dir)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == modelID {
			return &list[i], nil
		}
	}

	return nil, nil
}

// InjectIntoSchema sets properties.model.oneOf and properties.model.default
// on the decoded values.schema.json from the given models. It is a no-op when
// there are no models or the schema has no "model" property.
func InjectIntoSchema(schema map[string]any, models []types.Model) {
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

// ExtractFromSchema extracts model names from a JSON schema.
// It looks for properties with "model" in the key and extracts values from oneOf/const fields.
func ExtractFromSchema(schema map[string]any, modelSet map[string]bool) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}

	for key, value := range properties {
		if !strings.Contains(strings.ToLower(key), "model") {
			continue
		}

		propMap, ok := value.(map[string]any)
		if !ok {
			continue
		}

		extractFromProperty(propMap, modelSet)
	}
}

// extractFromProperty extracts model values from a property's oneOf array.
func extractFromProperty(propMap map[string]any, modelSet map[string]bool) {
	oneOf, ok := propMap["oneOf"].([]any)
	if !ok {
		return
	}
	for _, option := range oneOf {
		if optMap, ok := option.(map[string]any); ok {
			if constVal, ok := optMap["const"].(string); ok && constVal != "" {
				modelSet[constVal] = true
			}
		}
	}
}

// SetInfo sets the response's name and component type id once, from the first provider seen.
func SetInfo(result *types.ModelDeployOptions, name, componentType string) {
	if result.Name == "" {
		result.Name = name
	}
	if result.ComponentType.ID == "" {
		result.ComponentType.ID = componentType
	}
}

// SetComponentTypeNames fills the component type's display names from the components of that type.
func SetComponentTypeNames(ct *types.ComponentType, components []types.Component) {
	for _, c := range components {
		if c.ComponentType.ID != ct.ID {
			continue
		}
		if ct.Name == "" {
			ct.Name = c.ComponentType.Name
		}
		if ct.ShortName == "" {
			ct.ShortName = c.ComponentType.ShortName
		}
		if ct.Name != "" && ct.ShortName != "" {
			return
		}
	}
}

// BuildDeployProfiles resolves the profile ids listed by model against profilesByID.
// base carries the provider-level resources (CPU, Storage); each profile adds its own
// Memory and Accelerators on top. providerID is only used for error messages.
func BuildDeployProfiles(model *types.Model, profilesByID map[string]profiles.Profile, providerID string, base types.Resources) ([]types.ModelDeployProfile, error) {
	deployProfiles := make([]types.ModelDeployProfile, 0, len(model.Profiles))
	for _, id := range model.Profiles {
		pr, ok := profilesByID[id]
		if !ok {
			return nil, fmt.Errorf("model '%s' lists profile '%s' which is not defined in %s for provider '%s'", model.ID, id, profiles.FileName, providerID)
		}
		res := base // copy, so each profile gets its own resources
		res.Memory = pr.Params.Resources.Memory
		res.Accelerators = pr.Params.Resources.Accelerators

		deployProfiles = append(deployProfiles, types.ModelDeployProfile{
			ID:          pr.ID,
			Name:        pr.Name,
			Description: pr.Description,
			Default:     profiles.IsDefault(model.DefaultProfile, pr),
			Resources:   res,
		})
	}

	return deployProfiles, nil
}

// FilterAndSortProviders applies the provider filter, then sorts:
// local first, default first, then by id.
func FilterAndSortProviders(providers []types.ModelDeployProvider, providerID string) []types.ModelDeployProvider {
	filtered := make([]types.ModelDeployProvider, 0, len(providers))
	for _, pr := range providers {
		if providerID == "" || pr.ID == providerID {
			filtered = append(filtered, pr)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.DeploymentType != b.DeploymentType {
			return a.DeploymentType == constants.DeploymentTypeLocal
		}
		if a.Default != b.Default {
			return a.Default
		}

		return a.ID < b.ID
	})

	return filtered
}
