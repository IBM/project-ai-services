package openapi

import (
	"testing"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
)

func TestConvertSchemaToJSONSchema(t *testing.T) {
	tests := []struct {
		name        string
		schema      *base.SchemaProxy
		expectNil   bool
		checkFields func(*testing.T, interface{})
	}{
		{
			name:      "nil schema",
			schema:    nil,
			expectNil: true,
		},
		{
			name:      "schema proxy with nil schema",
			schema:    &base.SchemaProxy{},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertSchemaToJSONSchema(tt.schema)
			if tt.expectNil {
				if result != nil {
					t.Errorf("ConvertSchemaToJSONSchema() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Error("ConvertSchemaToJSONSchema() returned nil, want non-nil")
				}
				if tt.checkFields != nil {
					tt.checkFields(t, result)
				}
			}
		})
	}
}

func TestConvertSchemaToJSONSchemaWithValidSchema(t *testing.T) {
	// For this test, we'll verify that the function handles various nil conditions correctly
	// The actual schema building is tested through integration tests

	t.Run("nil handling", func(t *testing.T) {
		result := ConvertSchemaToJSONSchema(nil)
		if result != nil {
			t.Error("ConvertSchemaToJSONSchema(nil) should return nil")
		}
	})

	t.Run("empty proxy handling", func(t *testing.T) {
		proxy := &base.SchemaProxy{}
		result := ConvertSchemaToJSONSchema(proxy)
		if result != nil {
			t.Error("ConvertSchemaToJSONSchema(empty proxy) should return nil")
		}
	})
}

func TestConvertSchemaIntegration(t *testing.T) {
	// This test verifies the conversion works with actual OpenAPI documents
	doc, err := LoadDescription("testdata/complex.yaml", false)
	if err != nil {
		t.Fatalf("Failed to load test document: %v", err)
	}

	// Find an operation with a request body schema
	var foundSchema bool
	for pair := doc.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		pathItem := pair.Value()
		if pathItem.Put != nil && pathItem.Put.RequestBody != nil {
			rb := pathItem.Put.RequestBody
			if rb.Content != nil {
				for contentPair := rb.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
					mediaType := contentPair.Value()
					if mediaType.Schema != nil {
						schema := ConvertSchemaToJSONSchema(mediaType.Schema)
						if schema != nil {
							foundSchema = true
							// Basic validation that conversion worked
							if schema.Type == "" {
								t.Error("Converted schema has no type")
							}
						}
					}
				}
			}
		}
	}

	if !foundSchema {
		t.Error("No schemas found in test document")
	}
}
