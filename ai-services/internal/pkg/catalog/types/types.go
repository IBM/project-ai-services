package types

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

const (
	// yamlMappingKeyValuePairs represents that YAML mapping nodes store content as key-value pairs.
	yamlMappingKeyValuePairs = 2
)

// Architecture represents a complete AI solution template.
type Architecture struct {
	ID               string               `yaml:"id" json:"id"`
	Name             string               `yaml:"name" json:"name"`
	Description      string               `yaml:"description" json:"description"`
	Version          string               `yaml:"version" json:"version"`
	Type             string               `yaml:"type" json:"type"` // "architecture"
	CertifiedBy      string               `yaml:"certified_by" json:"certified_by"`
	Runtimes         []string             `yaml:"runtimes" json:"runtimes"`
	GlobalComponents []ComponentReference `yaml:"global_components,omitempty" json:"global_components,omitempty"`
	Services         []ServiceReference   `yaml:"services" json:"services"`
	Links            *ArchitectureLinks   `yaml:"links,omitempty" json:"links,omitempty"`
	About            *yaml.Node           `yaml:"-" json:"-"`
}

// MarshalJSON implements custom JSON marshaling for Architecture to properly handle yaml.Node.
func (a Architecture) MarshalJSON() ([]byte, error) {
	type Alias Architecture
	aux := &struct {
		About interface{} `json:"about,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(&a),
	}
	if a.About != nil && a.About.Kind != 0 {
		aux.About = yamlNodeToInterface(a.About) // Uses your helper below
	}

	return json.Marshal(aux)
}

// UnmarshalYAML handles raw document maps to safely capture nested array/map structure nodes.
func (a *Architecture) UnmarshalYAML(value *yaml.Node) error {
	type Alias Architecture
	aux := (*Alias)(a)

	// Temporarily nil out About to prevent unmarshal errors
	aux.About = nil

	// Decode all fields except About
	if err := value.Decode(aux); err != nil {
		return err
	}

	// Extract About node manually
	a.About = extractAboutNode(value)

	return nil
}

// ArchitectureSummary represents an architecture for list API responses.
type ArchitectureSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CertifiedBy string   `json:"certified_by"`
	Services    []string `json:"services"`
}

// ArchitectureLinks contains links related to an architecture.
type ArchitectureLinks struct {
	Demo          string `yaml:"demo,omitempty" json:"demo,omitempty"`
	Code          string `yaml:"code,omitempty" json:"code,omitempty"`
	Documentation string `yaml:"documentation,omitempty" json:"documentation,omitempty"`
}

// ServiceReference represents a reference to a service in an architecture.
type ServiceReference struct {
	ID       string `yaml:"id" json:"id"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// ComponentReference represents a reference to a component type.
type ComponentReference struct {
	Type string `yaml:"type" json:"type"`
}

// DependencyReference represents a reference to a dependency service.
type DependencyReference struct {
	ID string `yaml:"id" json:"id"`
}

// Resources represents resource requirements for a service or component.
type Resources struct {
	CPU          int            `yaml:"cpu,omitempty" json:"cpu,omitempty"`                   // CPU cores
	Memory       int            `yaml:"memory,omitempty" json:"memory,omitempty"`             // Memory in bytes
	Accelerators map[string]int `yaml:"accelerators,omitempty" json:"accelerators,omitempty"` // Accelerator cards (e.g., "ibm.com/spyre_pf": 1)
	Storage      int            `yaml:"storage,omitempty" json:"storage,omitempty"`           // Storage in bytes
}

// Service represents a deployable AI service.
type Service struct {
	ID            string                `yaml:"id" json:"id"`
	Name          string                `yaml:"name" json:"name"`
	Description   string                `yaml:"description" json:"description"`
	Type          string                `yaml:"type" json:"type"` // "service"
	CertifiedBy   string                `yaml:"certified_by" json:"certified_by"`
	Architectures []string              `yaml:"architectures" json:"architectures"`
	Dependencies  []DependencyReference `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Standalone    bool                  `yaml:"standalone,omitempty" json:"standalone,omitempty"`
	About         *yaml.Node            `yaml:"-" json:"-"`
}

// UnmarshalYAML extracts the 'about' node manually, insulating it from reflection errors.
func (s *Service) UnmarshalYAML(value *yaml.Node) error {
	type Alias Service
	aux := (*Alias)(s)

	// Temporarily nil out About to prevent unmarshal errors
	aux.About = nil

	// Decode all fields except About
	if err := value.Decode(aux); err != nil {
		return err
	}

	// Extract About node manually
	s.About = extractAboutNode(value)

	return nil
}

// MarshalJSON implements custom JSON marshaling for Service to properly handle yaml.Node.
func (s Service) MarshalJSON() ([]byte, error) {
	type Alias Service
	aux := &struct {
		About interface{} `json:"about,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(&s),
	}
	if s.About != nil && s.About.Kind != 0 {
		aux.About = yamlNodeToInterface(s.About)
	}

	return json.Marshal(aux)
}

// ServiceSummary represents a service for list API responses.
type ServiceSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	CertifiedBy   string   `json:"certified_by"`
	Architectures []string `json:"architectures"`
	Standalone    bool     `json:"standalone"`
}

// Component represents an infrastructure component (vector_store, embedding, llm, etc.).
type Component struct {
	ID            string `yaml:"id" json:"id"`
	Name          string `yaml:"name" json:"name"`
	Description   string `yaml:"description" json:"description"`
	Type          string `yaml:"type" json:"type"`                           // "component"
	ComponentType string `yaml:"component_type" json:"component_type"`       // "vector_store", "embedding", "llm", etc.
	ComponentName string `yaml:"component_name" json:"component_name"`       // Display name for component type (e.g., "Vector store", "Large language model")
	Default       bool   `yaml:"default,omitempty" json:"default,omitempty"` // Whether this is the default provider for this component type
}

// ComponentSummary represents a component for list API responses.
type ComponentSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ComponentType string `json:"component_type"`
}

// RuntimeMetadata contains runtime-specific metadata.
type RuntimeMetadata struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

// DeployOptionsProvider represents a provider for a component type.
type DeployOptionsProvider struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Version     string     `json:"version,omitempty"`
	Default     bool       `json:"default,omitempty"`
	Schema      string     `json:"schema,omitempty"`
	Resources   *Resources `json:"resources,omitempty"`
}

// DeployOptionsComponent represents a component type with its providers.
type DeployOptionsComponent struct {
	Type      string                  `json:"type"`
	Name      string                  `json:"name"`
	Providers []DeployOptionsProvider `json:"providers"`
}

// DeployOptionsService represents a service with its components.
type DeployOptionsService struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Version    string                   `json:"version,omitempty"`
	Schema     string                   `json:"schema,omitempty"`
	Components []DeployOptionsComponent `json:"components"`
	Resources  *Resources               `json:"resources,omitempty"`
}

// DeployOptionsArchitecture represents deploy options for an architecture.
type DeployOptionsArchitecture struct {
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	Version          string                   `json:"version,omitempty"`
	GlobalComponents []DeployOptionsComponent `json:"global_components"`
	Services         []DeployOptionsService   `json:"services"`
}

// extractAboutNode finds and returns the 'about' field from a YAML node.
func extractAboutNode(value *yaml.Node) *yaml.Node {
	mapNode := value
	if value.Kind == yaml.DocumentNode && len(value.Content) > 0 {
		mapNode = value.Content[0]
	}

	for i := 0; i < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == "about" && i+1 < len(mapNode.Content) {
			return mapNode.Content[i+1]
		}
	}

	return nil
}

// OrderedMap represents a map that preserves insertion order for JSON marshaling.
type OrderedMap []OrderedMapEntry

// OrderedMapEntry represents a single key-value pair in an ordered map.
type OrderedMapEntry struct {
	Key   string      `json:"-"`
	Value interface{} `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for OrderedMap to preserve key order.
func (om OrderedMap) MarshalJSON() ([]byte, error) {
	if len(om) == 0 {
		return []byte("{}"), nil
	}

	jsonBytes := []byte("{")
	for i, entry := range om {
		if i > 0 {
			jsonBytes = append(jsonBytes, ',')
		}

		keyJSON, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		jsonBytes = append(jsonBytes, keyJSON...)
		jsonBytes = append(jsonBytes, ':')

		valueJSON, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		jsonBytes = append(jsonBytes, valueJSON...)
	}
	jsonBytes = append(jsonBytes, '}')

	return jsonBytes, nil
}

// yamlNodeToInterface converts a yaml.Node to a native Go interface{} while preserving order.
func yamlNodeToInterface(node *yaml.Node) interface{} {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.DocumentNode:
		return yamlNodeToDocument(node)
	case yaml.SequenceNode:
		return yamlNodeToSequence(node)
	case yaml.MappingNode:
		return yamlNodeToMapping(node)
	case yaml.ScalarNode:
		return node.Value
	case yaml.AliasNode:
		return yamlNodeToInterface(node.Alias)
	default:
		return nil
	}
}

func yamlNodeToDocument(node *yaml.Node) interface{} {
	if len(node.Content) > 0 {
		return yamlNodeToInterface(node.Content[0])
	}

	return nil
}

func yamlNodeToSequence(node *yaml.Node) []interface{} {
	result := make([]interface{}, 0, len(node.Content))
	for _, item := range node.Content {
		result = append(result, yamlNodeToInterface(item))
	}

	return result
}

func yamlNodeToMapping(node *yaml.Node) OrderedMap {
	result := make(OrderedMap, 0, len(node.Content)/yamlMappingKeyValuePairs)
	for i := 0; i < len(node.Content); i += yamlMappingKeyValuePairs {
		key := node.Content[i].Value
		value := yamlNodeToInterface(node.Content[i+1])
		result = append(result, OrderedMapEntry{Key: key, Value: value})
	}

	return result
}

// Made with Bob
