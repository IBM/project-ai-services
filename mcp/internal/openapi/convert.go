package openapi

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	base "github.com/pb33f/libopenapi/datamodel/high/base"
	"gopkg.in/yaml.v3"
)

// ConvertSchemaToJSONSchema converts a libopenapi schema to a JSON Schema
func ConvertSchemaToJSONSchema(schema *base.SchemaProxy) *jsonschema.Schema {
	if schema == nil {
		return nil
	}

	// Build the schema
	built := schema.Schema()
	if built == nil {
		return nil
	}

	// Render the schema to YAML bytes and convert to map
	renderedYAML, _ := built.Render()
	if renderedYAML == nil {
		return nil
	}

	// Parse YAML to a map
	var schemaMap map[string]interface{}
	if err := yaml.Unmarshal(renderedYAML, &schemaMap); err != nil {
		return nil
	}

	// Marshal to JSON and unmarshal to jsonschema.Schema
	jsonData, err := json.Marshal(schemaMap)
	if err != nil {
		return nil
	}

	var js *jsonschema.Schema
	if err := json.Unmarshal(jsonData, &js); err != nil {
		return nil
	}

	return js
}
