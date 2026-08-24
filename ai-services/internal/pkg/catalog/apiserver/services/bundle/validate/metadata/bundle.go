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

// CatalogChecker provides catalog-state queries during metadata validation.
// It is satisfied by *catalog.CatalogProvider but defined here so neither the
// validate package nor its callers need to import the concrete catalog package.
type CatalogChecker interface {
	// ServiceExists reports whether a service with the given ID is registered
	// in the catalog (embedded or active bundle).
	ServiceExists(id string) bool
	// ComponentExists reports whether a component with the given type and ID is
	// registered in the catalog (embedded or active bundle).
	ComponentExists(componentType, id string) bool
}

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

// MetadataYAML is the single decode target for a root metadata.yaml.
// One struct covers all fields for both service and component bundles so
// there is no duplication when the schema evolves.
type MetadataYAML struct {
	ID            string        `yaml:"id"`
	Type          string        `yaml:"type"`
	Name          string        `yaml:"name"`
	Description   string        `yaml:"description"`
	Version       string        `yaml:"version"`
	ComponentType string        `yaml:"component_type"`
	Standalone    *bool         `yaml:"standalone"` // pointer — distinguishes absent from false
	Dependencies  []MetadataDep `yaml:"dependencies"`
}

// MetadataDep is a single entry in the dependencies list.
type MetadataDep struct {
	ID string `yaml:"id"`
}

// UnmarshalMetadataYAML decodes raw YAML bytes into MetadataYAML.
// Returns *validators.ValidationError{Code:400} on parse failure.
func UnmarshalMetadataYAML(data []byte) (*MetadataYAML, error) {
	var m MetadataYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to parse metadata.yaml: %s", err),
		}
	}

	return &m, nil
}

// ValidateRootMetadata checks all required fields of a decoded MetadataYAML and
// returns the first validation error encountered, or nil when the document is valid.
//
// Required for all types:   id, type, version, name, description.
// Required for service:     standalone.
// Required for component:   component_type.
//
// Returns *validators.ValidationError{Code:422} for missing/invalid fields or an
// unknown type value.
func ValidateRootMetadata(m *MetadataYAML) error {
	if err := validateCommonMetadataFields(m); err != nil {
		return err
	}

	return validateTypeSpecificFields(m)
}

// validateCommonMetadataFields checks the fields required for every bundle type.
func validateCommonMetadataFields(m *MetadataYAML) error {
	if m.ID == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'id' is required"}
	}
	if m.Type == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'type' is required"}
	}
	if m.Version == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'version' is required"}
	}
	if strings.TrimSpace(m.Name) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'name' is required"}
	}
	if strings.TrimSpace(m.Description) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'description' is required"}
	}

	return nil
}

// validateTypeSpecificFields checks fields that are only required for a specific bundle type.
func validateTypeSpecificFields(m *MetadataYAML) error {
	switch m.Type {
	case CatalogTypeService:
		if m.Standalone == nil {
			return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "metadata.yaml: 'standalone' is required for type=service"}
		}
	case CatalogTypeComponent:
		if m.ComponentType == "" {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: "metadata.yaml: 'component_type' is required for type=component",
			}
		}
	default:
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("metadata.yaml: unsupported type %q (expected %q or %q)", m.Type, CatalogTypeService, CatalogTypeComponent),
		}
	}

	return nil
}
