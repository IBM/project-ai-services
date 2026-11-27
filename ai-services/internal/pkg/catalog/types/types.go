package types

import (
	"encoding/json"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"go.yaml.in/yaml/v3"
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
		About: aboutValue(a.About),
	}

	return json.Marshal(aux)
}

// UnmarshalYAML handles raw document maps to safely capture nested array/map structure nodes.
func (a *Architecture) UnmarshalYAML(value *yaml.Node) error {
	type Alias Architecture

	return decodeWithAbout(value, (*Alias)(a), &a.About)
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

// DatasourceSettings defines compatibility and lifecycle settings for datasources attached to a service.
// AllowConnect and AllowDisconnect are pointers so an unset value (nil, default applies) can be told
// apart from an explicit false.
type DatasourceSettings struct {
	// Required indicates that at least one datasource must be connected before the application can be used.
	Required bool `yaml:"required,omitempty" json:"required"`
	// AllowConnect controls whether datasources may be connected after application creation via the connect API.
	// Defaults to true when unset.
	AllowConnect *bool `yaml:"allow_connect,omitempty" json:"allow_connect,omitempty"`
	// AllowDisconnect controls whether datasources may be disconnected after application creation via the disconnect API.
	// Defaults to false when required is true, and true otherwise.
	AllowDisconnect *bool `yaml:"allow_disconnect,omitempty" json:"allow_disconnect,omitempty"`
	// SupportedProviders is the list of datasource provider IDs that this service accepts (e.g. "s3", "filesystem").
	// When empty, all providers are accepted.
	SupportedProviders []string `yaml:"supported_providers,omitempty" json:"supported_providers,omitempty"`
	// MaxConnections is the maximum number of datasources that may be attached to the service at any
	// one time (creation-time and post-creation connections combined). nil means unlimited.
	// Must be >= 1 when set.
	MaxConnections *int `yaml:"max_connections,omitempty" json:"max_connections,omitempty"`
}

// CanConnect reports whether datasources may be connected post-application creation.
// allow_connect defaults to true when unset.
func (s *DatasourceSettings) CanConnect() bool {
	return s.AllowConnect == nil || *s.AllowConnect
}

// CanDisconnect reports whether datasources may be disconnected post-application creation.
// allow_disconnect defaults to false when the datasource is required and to true otherwise.
func (s *DatasourceSettings) CanDisconnect() bool {
	if s.AllowDisconnect == nil {
		return !s.Required
	}

	return *s.AllowDisconnect
}

// ResolveDefaults returns a copy of the settings with allow_connect and allow_disconnect
// set to their effective values so API consumers never see an unset flag.
// It returns nil when the service declares no datasource block (s == nil).
func (s *DatasourceSettings) ResolveDefaults() *DatasourceSettings {
	if s == nil {
		return nil
	}

	allowConnect := s.CanConnect()
	allowDisconnect := s.CanDisconnect()
	resolved := *s
	resolved.AllowConnect = &allowConnect
	resolved.AllowDisconnect = &allowDisconnect

	return &resolved
}

// ConnectPolicy controls whether the allow_connect catalog flag is checked when
// linking datasources to an application.
type ConnectPolicy bool

// Datasource connect policy constants.
const (
	// EnforceAllowConnect rejects the connect request with a 422 if any eligible
	// service has allow_connect == false in its catalog settings. Use for the
	// PUT /applications/:id/datasources HTTP endpoint.
	EnforceAllowConnect ConnectPolicy = true
	// SkipAllowConnect bypasses the allow_connect check. Use for the
	// application-creation deploy path where datasources are always attached.
	SkipAllowConnect ConnectPolicy = false
)

// Service represents a deployable AI service.
type Service struct {
	ID                string                `yaml:"id" json:"id"`
	Name              string                `yaml:"name" json:"name"`
	Description       string                `yaml:"description" json:"description"`
	Type              string                `yaml:"type" json:"type"` // "service"
	CertifiedBy       string                `yaml:"certified_by" json:"certified_by"`
	Architectures     []string              `yaml:"architectures" json:"architectures"`
	Dependencies      []DependencyReference `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Standalone        bool                  `yaml:"standalone,omitempty" json:"standalone,omitempty"`
	AcceptsDatasource bool                  `yaml:"-" json:"accepts_datasource,omitempty"`
	Datasource        *DatasourceSettings   `yaml:"datasource,omitempty" json:"datasource,omitempty"`
	About             *yaml.Node            `yaml:"-" json:"-"`
}

// UnmarshalYAML extracts the 'about' node manually, insulating it from reflection errors,
// and derives AcceptsDatasource from the presence of the datasource block.
func (s *Service) UnmarshalYAML(value *yaml.Node) error {
	type Alias Service

	if err := decodeWithAbout(value, (*Alias)(s), &s.About); err != nil {
		return err
	}

	if s.Datasource != nil {
		s.AcceptsDatasource = true
	}

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
		About: aboutValue(s.About),
	}

	return json.Marshal(aux)
}

// ServiceSummary represents a service for list API responses.
type ServiceSummary struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	CertifiedBy   string                `json:"certified_by"`
	Architectures []string              `json:"architectures"`
	Standalone    bool                  `json:"standalone"`
	Dependencies  []DependencyReference `json:"dependencies,omitempty"`
}

// Component represents an infrastructure component (vector_store, embedding, llm, etc.).
type Component struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Type        string `yaml:"type" json:"type"` // "component"
	// ComponentType is the component type this provider belongs to ("vector_store", "embedding", "llm", etc.).
	ComponentType ComponentType `yaml:"component_type" json:"component_type"`
	Default       bool          `yaml:"default,omitempty" json:"default,omitempty"` // Whether this is the default provider for this component type
	// Category groups components by purpose, e.g. "models" for components that serve a model.
	Category string `yaml:"category,omitempty" json:"category,omitempty"`
	// LiteLLM describes how a "models" component is exposed through the LiteLLM gateway.
	LiteLLM *LiteLLMConfig `yaml:"litellm,omitempty" json:"-"`
}

// LiteLLMConfig is the gateway registration settings of a model component provider.
type LiteLLMConfig struct {
	Provider    string `yaml:"provider"`      // LiteLLM provider prefix, e.g. "hosted_vllm"
	ModelMode   string `yaml:"model_mode"`    // LiteLLM model mode, e.g. "chat", "embedding", "rerank"
	APIBasePath string `yaml:"api_base_path"` // path appended to the model server's address, e.g. "/v1"
}

// Model is one entry in a provider's models.yaml.
type Model struct {
	ID             string     `yaml:"id" json:"id"`                   // full model name, e.g. "ibm-granite/granite-4.1-8b-fp8"
	Default        bool       `yaml:"default" json:"default"`         // whether this is the default model for the provider
	Title          string     `yaml:"title" json:"title"`             // short display name
	Name           string     `yaml:"name" json:"name"`               // human-readable name
	Description    string     `yaml:"description" json:"description"` // markdown description
	License        string     `yaml:"license" json:"-"`               // e.g. "Apache-2.0"
	CertifiedBy    string     `yaml:"certified_by" json:"-"`          // e.g. "IBM"
	About          *yaml.Node `yaml:"-" json:"-"`                     // free-form details, kept as YAML
	ComponentType  string     `yaml:"component_type" json:"-"`        // model role; set in connector models.yaml only
	DefaultProfile string     `yaml:"default_profile" json:"-"`       // default hardware profile ID; local providers only
	Profiles       []string   `yaml:"profiles" json:"-"`              // supported hardware profile IDs; local providers only
}

// ModelCatalogEntry is a model as GET /catalog/models returns it: the model with the providers that serve it.
type ModelCatalogEntry struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	ComponentType ComponentType          `json:"component_type"`
	CertifiedBy   string                 `json:"certified_by"`
	Description   string                 `json:"description"`
	Providers     []ModelCatalogProvider `json:"providers"`
}

// ModelCatalogProvider is a provider listed on a ModelCatalogEntry.
type ModelCatalogProvider struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	DeploymentType constants.DeploymentType `json:"deployment_type"` // "local" or "remote"
}

// ModelDetail is a model as GET /catalog/models/:id returns it: the list entry plus license and about.
type ModelDetail struct {
	ModelCatalogEntry
	License string `json:"license,omitempty"`
	About   any    `json:"about,omitempty"`
}

// UnmarshalYAML keeps the free-form 'about' node, as Service and Architecture do.
func (m *Model) UnmarshalYAML(value *yaml.Node) error {
	type Alias Model

	return decodeWithAbout(value, (*Alias)(m), &m.About)
}

// Entry returns the model's own fields of its list entry; the caller fills in the component type and providers.
func (m Model) Entry() ModelCatalogEntry {
	return ModelCatalogEntry{
		ID:          m.ID,
		Name:        m.Name,
		CertifiedBy: m.CertifiedBy,
		Description: m.Description,
	}
}

// Detail returns the model's own fields of its get-by-id view; the caller fills in the component type and providers.
func (m Model) Detail() ModelDetail {
	return ModelDetail{
		ModelCatalogEntry: m.Entry(),
		License:           m.License,
		About:             aboutValue(m.About),
	}
}

// ComponentSummary represents a component for list API responses.
type ComponentSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ComponentType string `json:"component_type"`
}

// Connector holds the metadata loaded from a connector's metadata.yaml file.
type Connector struct {
	Type          string `yaml:"type"`           // always "connector"
	ID            string `yaml:"id"`             // e.g. "object_storage", "file_system"
	Name          string `yaml:"name"`           // provider display name
	Description   string `yaml:"description"`    // short description
	ConnectorType string `yaml:"connector_type"` // e.g. "datasource"
	ConnectorName string `yaml:"connector_name"` // display label for connector_type, e.g. "Data sources"
}

// ConnectorProvider holds the provider-level fields nested inside ConnectorResponse.
type ConnectorProvider struct {
	// ID is the provider identifier (e.g. "object_storage", "file_system").
	ID string `json:"id"`
	// Name is the human-readable display name of the provider.
	Name string `json:"name"`
	// Description is a short description of the provider.
	Description string `json:"description"`
	// Schema is the path to the provider's JSON Schema params endpoint.
	Schema string `json:"schema"`
}

// ConnectorResponse is the API response shape for a connector entry.
type ConnectorResponse struct {
	// Type is the connector type (e.g. "datasource").
	Type string `json:"type"`
	// Name is the display label for the connector type (e.g. "Data sources").
	Name string `json:"name"`
	// Provider contains the identity and schema URL of the connector provider.
	Provider ConnectorProvider `json:"provider"`
}

// ToConnectorResponse converts a Connector into the API response shape.
func ToConnectorResponse(c *Connector) ConnectorResponse {
	return ConnectorResponse{
		Type: c.ConnectorType,
		Name: c.ConnectorName,
		Provider: ConnectorProvider{
			ID:          c.ID,
			Name:        c.Name,
			Description: c.Description,
			Schema:      "/api/v1/connectors/" + c.ConnectorType + "/providers/" + c.ID + "/params",
		},
	}
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
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	Version           string                   `json:"version,omitempty"`
	Schema            string                   `json:"schema,omitempty"`
	Components        []DeployOptionsComponent `json:"components"`
	Resources         *Resources               `json:"resources,omitempty"`
	AcceptsDatasource bool                     `json:"accepts_datasource,omitempty"`
	Datasource        *DatasourceSettings      `json:"datasource,omitempty"`
}

// ModelDeployProfile is a hardware profile a model can be deployed with.
type ModelDeployProfile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Default     bool      `json:"default"`
	Resources   Resources `json:"resources"`
}

// ModelDeployProvider is a provider (local or remote) a model can be deployed through.
type ModelDeployProvider struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	DeploymentType constants.DeploymentType `json:"deployment_type"` // "local" or "remote"
	Default        bool                     `json:"default"`
	Profiles       []ModelDeployProfile     `json:"profiles,omitempty"`
	Schema         string                   `json:"schema,omitempty"`
}

// ComponentType describes a component type (llm, embedding, vector_store, ...) by id and display names.
type ComponentType struct {
	ID        string `yaml:"id" json:"id"`
	Name      string `yaml:"name" json:"name"`
	ShortName string `yaml:"short_name" json:"short_name"`
}

// UnmarshalYAML accepts either the block form (id/name/short_name) or a bare scalar id (e.g. `llm`),
// the latter being what user-uploaded bundles use.
// TODO(BYOS): remove the scalar branch once bundle metadata.yaml moves to the block form.
func (m *ComponentType) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		m.ID = value.Value

		return nil
	}

	type plain ComponentType

	return value.Decode((*plain)(m))
}

// ModelDeployOptions is the response of GET /catalog/models/:id/deploy-options.
type ModelDeployOptions struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	ComponentType ComponentType         `json:"component_type"`
	Providers     []ModelDeployProvider `json:"providers"`
}

// DeployOptionsArchitecture represents deploy options for an architecture.
type DeployOptionsArchitecture struct {
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	Version          string                   `json:"version,omitempty"`
	GlobalComponents []DeployOptionsComponent `json:"global_components"`
	Services         []DeployOptionsService   `json:"services"`
}

// decodeWithAbout decodes value into dst, then stores the free-form 'about' node in about.
// dst must be an alias of the caller's type, so decoding does not call the caller's UnmarshalYAML again.
func decodeWithAbout(value *yaml.Node, dst any, about **yaml.Node) error {
	if err := value.Decode(dst); err != nil {
		return err
	}

	*about = extractAboutNode(value)

	return nil
}

// aboutValue converts an 'about' node to a JSON-friendly value, or nil if there is none.
func aboutValue(node *yaml.Node) any {
	if node == nil || node.Kind == 0 {
		return nil
	}

	return yamlNodeToInterface(node)
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
