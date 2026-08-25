// Package bundlemetadata defines the metadata types, catalog-type constants, and
// YAML decode/validation helpers for catalog bundle archives.
// It is intentionally free of any dependency on the parent validate package so
// that both can import this package without cycles.
package bundlemetadata

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"go.yaml.in/yaml/v3"
)

// Catalog type constants shared across the bundle pipeline.
const (
	CatalogTypeService   = "service"
	CatalogTypeComponent = "component"
)

// ServiceMetadata holds the parsed identity fields for a service bundle.
type ServiceMetadata struct {
	ID          string
	Type        string
	Ver         string
	DisplayName string
}

// ComponentMetadata holds the parsed identity fields for a component bundle.
// The DB catalog_id for a component is the composite "<ComponentType>--<ID>".
type ComponentMetadata struct {
	ID            string
	Type          string
	ComponentType string
	Ver           string
	DisplayName   string
}

// ServiceMetadataYAML is the decode target for a service bundle's root metadata.yaml.
// It only exposes fields that are valid for type=service.
type ServiceMetadataYAML struct {
	ID           string        `yaml:"id"`
	Type         string        `yaml:"type"`
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	Version      string        `yaml:"version"`
	Standalone   *bool         `yaml:"standalone"` // pointer — distinguishes absent from false
	Dependencies []MetadataDep `yaml:"dependencies"`
	About        []any         `yaml:"about"`
}

// ComponentMetadataYAML is the decode target for a component bundle's root metadata.yaml.
// It only exposes fields that are valid for type=component.
type ComponentMetadataYAML struct {
	ID            string `yaml:"id"`
	Type          string `yaml:"type"`
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Version       string `yaml:"version"`
	ComponentType string `yaml:"component_type"`
}

// MetadataDep is a single entry in the dependencies list.
type MetadataDep struct {
	ID string `yaml:"id"`
}

// typeProbe is the minimal decode target used to read just the `type` field
// so ParseRootMetadata can choose the correct typed struct for the second decode.
type typeProbe struct {
	Type string `yaml:"type"`
}

// ParseRootMetadata decodes raw YAML bytes and validates all required fields.
// It returns either a *ServiceMetadataYAML or *ComponentMetadataYAML depending
// on the `type` field, ensuring each typed struct only exposes its own fields.
//
// Required for all types:   id, type, version, name, description.
// Required for service:     standalone, about.
// Required for component:   component_type.
//
// Returns *validators.ValidationError{Code:400} on parse failure and
// *validators.ValidationError{Code:422} for missing/invalid fields or unknown type.
func ParseRootMetadata(data []byte) (any, error) {
	var probe typeProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to parse metadata.yaml: %s", err),
		}
	}

	switch probe.Type {
	case CatalogTypeService:
		return parseServiceMetadataYAML(data)
	case CatalogTypeComponent:
		return parseComponentMetadataYAML(data)
	case "":
		return nil, &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'type' is required"}
	default:
		return nil, &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("metadata.yaml: unsupported type %q (expected %q or %q)", probe.Type, CatalogTypeService, CatalogTypeComponent),
		}
	}
}

func parseServiceMetadataYAML(data []byte) (*ServiceMetadataYAML, error) {
	var m ServiceMetadataYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to parse metadata.yaml: %s", err),
		}
	}
	if err := validateCommonFields(m.ID, m.Type, m.Version, m.Name, m.Description); err != nil {
		return nil, err
	}
	if m.Standalone == nil {
		return nil, &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'standalone' is required for type=service"}
	}
	if len(m.About) == 0 {
		return nil, &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'about' is required for type=service"}
	}
	return &m, nil
}

func parseComponentMetadataYAML(data []byte) (*ComponentMetadataYAML, error) {
	var m ComponentMetadataYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to parse metadata.yaml: %s", err),
		}
	}
	if err := validateCommonFields(m.ID, m.Type, m.Version, m.Name, m.Description); err != nil {
		return nil, err
	}
	if m.ComponentType == "" {
		return nil, &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'component_type' is required for type=component"}
	}
	return &m, nil
}

// validateCommonFields checks the fields required for every bundle type.
func validateCommonFields(id, typ, version, name, description string) error {
	if id == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'id' is required"}
	}
	if typ == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'type' is required"}
	}
	if version == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'version' is required"}
	}
	if strings.TrimSpace(name) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'name' is required"}
	}
	if strings.TrimSpace(description) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'description' is required"}
	}
	return nil
}
