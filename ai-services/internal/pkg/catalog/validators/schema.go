package validators

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateParams validates parameters against a JSON schema.
func ValidateParams(params map[string]any, schema map[string]any, contextName string) error {
	// If no params provided or schema is empty, skip validation
	if len(params) == 0 || len(schema) == 0 {
		return nil
	}

	// First, validate that all provided params are supported by the schema
	if err := ValidateSupportedParams(params, schema, contextName); err != nil {
		return err
	}

	// Compile and validate against JSON schema
	compiledSchema, err := compileJSONSchema(schema, contextName)
	if err != nil {
		return err
	}

	// Validate params against schema
	return validateAgainstSchema(compiledSchema, params, contextName)
}

// compileJSONSchema prepares and compiles a JSON schema for validation.
func compileJSONSchema(schema map[string]any, contextName string) (*jsonschema.Schema, error) {
	// Wrap the schema in a proper JSON Schema structure if it doesn't have $schema
	fullSchema := schema
	if _, hasSchema := schema["$schema"]; !hasSchema {
		fullSchema = map[string]any{
			"$schema": "https://json-schema.org/draft-07/schema#",
			"type":    "object",
		}
		maps.Copy(fullSchema, schema)
	}

	// Convert schema map to JSON bytes
	schemaBytes, err := json.Marshal(fullSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema for %s: %v", contextName, err)
	}

	// Unmarshal the schema bytes into an interface for the compiler
	var schemaInterface any
	if err := json.Unmarshal(schemaBytes, &schemaInterface); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema for %s: %v", contextName, err)
	}

	// Compile the JSON schema
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schemaInterface); err != nil {
		return nil, fmt.Errorf("failed to add schema resource for %s: %v", contextName, err)
	}

	compiledSchema, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema for %s: %v", contextName, err)
	}

	return compiledSchema, nil
}

// validateAgainstSchema validates parameters against a compiled JSON schema.
func validateAgainstSchema(compiledSchema *jsonschema.Schema, params map[string]any, contextName string) error {
	if err := compiledSchema.Validate(params); err != nil {
		var errorMessages []string
		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			errorMessages = ExtractValidationErrors(validationErr)
		} else {
			errorMessages = []string{err.Error()}
		}

		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Parameter validation failed for %s: %s", contextName, strings.Join(errorMessages, "; ")),
		}
	}

	return nil
}

// ValidateSupportedParams checks if all provided parameters are defined in the schema.
// This ensures users don't provide unsupported parameters that would be silently ignored.
func ValidateSupportedParams(params map[string]any, schema map[string]any, contextName string) error {
	// Extract the properties defined in the schema
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		// If no properties defined in schema, no params should be provided
		if len(params) > 0 {
			paramKeys := make([]string, 0, len(params))
			for key := range params {
				paramKeys = append(paramKeys, key)
			}

			return &ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("Unsupported parameters for %s: %s. No parameters are supported for this resource.", contextName, strings.Join(paramKeys, ", ")),
			}
		}

		return nil
	}

	// Check each provided parameter against the schema properties
	var unsupportedParams []string
	for paramKey := range params {
		if err := ValidateNestedParam(paramKey, params[paramKey], properties); err != nil {
			unsupportedParams = append(unsupportedParams, paramKey)
		}
	}

	if len(unsupportedParams) > 0 {
		// Get list of supported parameters for helpful error message
		supportedParams := make([]string, 0, len(properties))
		for key := range properties {
			supportedParams = append(supportedParams, key)
		}

		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Unsupported parameters for %s: %s. Supported parameters are: %s", contextName, strings.Join(unsupportedParams, ", "), strings.Join(supportedParams, ", ")),
		}
	}

	return nil
}

// ValidateNestedParam validates a parameter key against schema properties, handling nested objects.
func ValidateNestedParam(paramKey string, paramValue any, properties map[string]any) error {
	// Check if the parameter exists in the schema properties
	propSchema, exists := properties[paramKey]
	if !exists {
		return fmt.Errorf("parameter '%s' not found in schema", paramKey)
	}

	// If the parameter value is a map (nested object), validate its nested properties
	if nestedParams, ok := paramValue.(map[string]any); ok {
		propSchemaMap, ok := propSchema.(map[string]any)
		if !ok {
			return nil // Schema doesn't define nested structure, let JSON schema validator handle it
		}

		// Check if the property schema defines nested properties
		nestedProperties, ok := propSchemaMap["properties"].(map[string]any)
		if !ok || len(nestedProperties) == 0 {
			return nil // No nested properties defined, let JSON schema validator handle it
		}

		// Validate each nested parameter
		for nestedKey := range nestedParams {
			if _, exists := nestedProperties[nestedKey]; !exists {
				return fmt.Errorf("nested parameter '%s.%s' not found in schema", paramKey, nestedKey)
			}
		}
	}

	return nil
}

// ExtractValidationErrors recursively extracts all validation error messages.
func ExtractValidationErrors(err *jsonschema.ValidationError) []string {
	var messages []string

	// Add current error message using Error() method
	if err.Error() != "" {
		messages = append(messages, err.Error())
	}

	// Recursively add causes
	for _, cause := range err.Causes {
		messages = append(messages, ExtractValidationErrors(cause)...)
	}

	return messages
}

// Made with Bob
