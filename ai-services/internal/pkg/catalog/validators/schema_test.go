package validators

import (
	"strings"
	"testing"
)

// Common test data
var (
	validAPIKey    = "5th8Ko-T-QGDCp7r0AR3mf9UEsSmKsC4k_nd87hytgXY"
	validProjectID = "964263f4-5ae0-41e4-8030-884b77181552"
	validURL       = "https://api.watsonx.ibm.com"
)

// getWatsonxSchema returns the watsonx validation schema
func getWatsonxSchema() map[string]any {
	return map[string]any{
		"$schema":  "https://json-schema.org/draft-07/schema#",
		"type":     "object",
		"required": []string{"watsonxApiKey", "watsonxProjectId", "watsonxUrl"},
		"properties": map[string]any{
			"watsonxApiKey": map[string]any{
				"type":      "string",
				"format":    "password",
				"pattern":   "^[A-Za-z0-9_-]{44}$",
				"minLength": 44,
				"maxLength": 44,
			},
			"watsonxProjectId": map[string]any{
				"type":    "string",
				"pattern": "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
			},
			"watsonxUrl": map[string]any{
				"type":    "string",
				"pattern": "^https://[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*(/.*)?$",
			},
		},
	}
}

// makeParams creates a parameter map with the given values
func makeParams(apiKey, projectID, url string) map[string]any {
	params := make(map[string]any)
	if apiKey != "" {
		params["watsonxApiKey"] = apiKey
	}
	if projectID != "" {
		params["watsonxProjectId"] = projectID
	}
	if url != "" {
		params["watsonxUrl"] = url
	}
	return params
}

func TestValidateParams_WatsonxUrlPattern(t *testing.T) {
	schema := getWatsonxSchema()

	tests := []struct {
		name      string
		params    map[string]any
		wantError bool
		errorMsg  string
	}{
		// Valid cases
		{
			name:      "valid all fields with proper formats",
			params:    makeParams(validAPIKey, validProjectID, validURL),
			wantError: false,
		},
		{
			name:      "valid with regional URL",
			params:    makeParams(validAPIKey, validProjectID, "https://us-south.ml.cloud.ibm.com/ml/v1"),
			wantError: false,
		},
		{
			name:      "valid with complex path",
			params:    makeParams(validAPIKey, validProjectID, "https://watsonx-api.example.com/v1/models"),
			wantError: false,
		},

		// API Key validation
		{
			name:      "invalid api key - too short",
			params:    makeParams("short-key", validProjectID, validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid api key - too long",
			params:    makeParams("4dw0Rd-T-QGDCp7r0AR3mf9UEsSmKsC4k_n1e96IFT1X-extra", validProjectID, validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid api key - invalid characters",
			params:    makeParams("4dw0Rd@T#QGDCp7r0AR3mf9UEsSmKsC4k_n1e96IFT1X", validProjectID, validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},

		// Project ID validation
		{
			name:      "invalid project ID - not a UUID",
			params:    makeParams(validAPIKey, "not-a-valid-uuid", validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid project ID - uppercase UUID",
			params:    makeParams(validAPIKey, "964263F4-5AE0-41E4-8030-884B77181552", validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid project ID - missing hyphens",
			params:    makeParams(validAPIKey, "964263f45ae041e48030884b77181552", validURL),
			wantError: true,
			errorMsg:  "does not match pattern",
		},

		// URL validation
		{
			name:      "invalid URL - http instead of https",
			params:    makeParams(validAPIKey, validProjectID, "http://api.watsonx.ibm.com"),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid URL - missing protocol",
			params:    makeParams(validAPIKey, validProjectID, "api.watsonx.ibm.com"),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name:      "invalid URL - ftp protocol",
			params:    makeParams(validAPIKey, validProjectID, "ftp://api.watsonx.ibm.com"),
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		// Missing required fields
		{
			name: "missing watsonxApiKey",
			params: map[string]any{
				"watsonxProjectId": validProjectID,
				"watsonxUrl":       validURL,
			},
			wantError: true,
			errorMsg:  "missing property 'watsonxApiKey'",
		},
		{
			name: "missing watsonxProjectId",
			params: map[string]any{
				"watsonxApiKey": validAPIKey,
				"watsonxUrl":    validURL,
			},
			wantError: true,
			errorMsg:  "missing property 'watsonxProjectId'",
		},
		{
			name: "missing watsonxUrl",
			params: map[string]any{
				"watsonxApiKey":    validAPIKey,
				"watsonxProjectId": validProjectID,
			},
			wantError: true,
			errorMsg:  "missing property 'watsonxUrl'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(tt.params, schema, "watsonx")

			// Validate error expectations
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none (expected error containing %q)", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("error message mismatch:\n  want substring: %q\n  got: %v", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateParams_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		params    map[string]any
		schema    map[string]any
		wantError bool
	}{
		{
			name:      "empty schema allows any params",
			params:    map[string]any{"someKey": "someValue"},
			schema:    map[string]any{},
			wantError: false,
		},
		{
			name:   "empty params with optional schema",
			params: map[string]any{},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
			wantError: false,
		},
		{
			name:   "nil params treated as empty",
			params: nil,
			schema: map[string]any{
				"type": "object",
			},
			wantError: false,
		},
		{
			name:      "nil schema allows any params",
			params:    map[string]any{"key": "value"},
			schema:    nil,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(tt.params, tt.schema, "test")
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateParams() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateParams_TypeValidation(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"stringField": map[string]any{"type": "string"},
			"numberField": map[string]any{"type": "number"},
			"boolField":   map[string]any{"type": "boolean"},
			"arrayField": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
	}

	tests := []struct {
		name      string
		params    map[string]any
		wantError bool
	}{
		{
			name: "valid types",
			params: map[string]any{
				"stringField": "test",
				"numberField": 42,
				"boolField":   true,
				"arrayField":  []any{"a", "b"},
			},
			wantError: false,
		},
		{
			name: "invalid string type",
			params: map[string]any{
				"stringField": 123,
			},
			wantError: true,
		},
		{
			name: "invalid number type",
			params: map[string]any{
				"numberField": "not a number",
			},
			wantError: true,
		},
		{
			name: "invalid boolean type",
			params: map[string]any{
				"boolField": "not a bool",
			},
			wantError: true,
		},
		{
			name: "invalid array item type",
			params: map[string]any{
				"arrayField": []any{"valid", 123},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(tt.params, schema, "test")
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateParams() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// Made with Bob
