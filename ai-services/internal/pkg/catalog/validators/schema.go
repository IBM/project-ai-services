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
// This function relies entirely on the JSON Schema validator to handle all validation logic,
// including parameter existence, types, constraints, and conditional requirements.
func ValidateParams(params map[string]any, schema map[string]any, contextName string) error {
	// If no params provided or schema is empty, skip validation
	if len(params) == 0 || len(schema) == 0 {
		return nil
	}

	// Compile and validate against JSON schema
	// The JSON Schema validator handles everything:
	// - Parameter existence (via additionalProperties: false in schema)
	// - Required/optional fields
	// - Conditional requirements (dependencies, oneOf, anyOf, allOf, if/then/else)
	// - Type validation
	// - Format validation
	// - Constraint validation (minLength, maxLength, min, max, pattern, etc.)
	compiledSchema, err := compileJSONSchema(schema, contextName)
	if err != nil {
		return err
	}

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
