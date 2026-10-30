package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

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

// modelDisplayName returns the model's name, or its ID when no name is set.
func modelDisplayName(m types.Model) string {
	if m.Name != "" {
		return m.Name
	}

	return m.ID
}

// modelAboutGroup and modelAboutSection mirror the shape of a service's "about"
// block: groups of titled sections holding either a single value or a list of values.
type modelAboutGroup struct {
	Title    string              `yaml:"title"`
	Sections []modelAboutSection `yaml:"sections"`
}

type modelAboutSection struct {
	Title  string   `yaml:"title"`
	Value  string   `yaml:"value"`
	Values []string `yaml:"values"`
}

// modelAboutParagraphs renders every section of the model's about block as a
// "**<section title>**: a, b, c." paragraph.
func modelAboutParagraphs(about *yaml.Node) []string {
	if about == nil {
		return nil
	}

	var groups []modelAboutGroup
	if err := about.Decode(&groups); err != nil {
		return nil
	}

	var paragraphs []string
	for _, g := range groups {
		for _, sec := range g.Sections {
			items := sec.Values
			if sec.Value != "" {
				items = append([]string{sec.Value}, items...)
			}
			if sec.Title == "" || len(items) == 0 {
				continue
			}
			paragraphs = append(paragraphs, "**"+sec.Title+"**: "+strings.Join(items, ", ")+".")
		}
	}

	return paragraphs
}

// modelSchemaDescription builds the markdown description exposed through the
// "oneOf" entry of the model property: the short description followed by the
// about paragraphs.
func modelSchemaDescription(m types.Model) string {
	var paragraphs []string
	if m.Description != "" {
		paragraphs = append(paragraphs, m.Description)
	}
	paragraphs = append(paragraphs, modelAboutParagraphs(m.About)...)

	return strings.Join(paragraphs, "\n\n")
}

// injectModelsIntoSchema populates properties.model of the decoded
// values.schema.json from the given models: oneOf (const/title/description) and default.
func injectModelsIntoSchema(schema map[string]any, models []types.Model, fallbackTitle string) {
	if len(models) == 0 {
		return
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		schema["properties"] = props
	}

	modelField, ok := props["model"].(map[string]any)
	if !ok {
		modelField = map[string]any{"type": "string"}
		if fallbackTitle != "" {
			modelField["title"] = fallbackTitle
			modelField["description"] = fallbackTitle
		}
		props["model"] = modelField
	}

	oneOf := make([]any, 0, len(models))
	defaultID := ""
	for _, m := range models {
		oneOf = append(oneOf, map[string]any{
			"const":       m.ID,
			"title":       modelDisplayName(m),
			"description": modelSchemaDescription(m),
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
