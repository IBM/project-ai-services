package openapi

import (
	"strings"
	"testing"

	"github.com/project-ai-services/mcp/internal/types"
)

func TestCanonicalizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "Test API",
			expected: "test-api",
		},
		{
			name:     "with special characters",
			input:    "Test@API#2.0",
			expected: "testapi20",
		},
		{
			name:     "multiple spaces",
			input:    "Test   API   Service",
			expected: "test-api-service",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  Test API  ",
			expected: "test-api",
		},
		{
			name:     "already lowercase",
			input:    "test-api",
			expected: "test-api",
		},
		{
			name:     "mixed case with numbers",
			input:    "MyAPI123",
			expected: "myapi123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeName(tt.input)
			if result != tt.expected {
				t.Errorf("canonicalizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewInterface(t *testing.T) {
	// Load test documents
	minimalDoc, err := LoadDescription("testdata/minimal.yaml")
	if err != nil {
		t.Fatalf("Failed to load minimal spec: %v", err)
	}

	complexDoc, err := LoadDescription("testdata/complex.yaml")
	if err != nil {
		t.Fatalf("Failed to load complex spec: %v", err)
	}

	t.Run("minimal spec", func(t *testing.T) {
		intf := NewInterface(minimalDoc)

		if intf.Doc == nil {
			t.Error("Interface.Doc is nil")
		}

		// Should have at least one operation
		if len(intf.Operations) == 0 {
			t.Error("Interface should have at least one operation")
		}

		// Check the operation
		if len(intf.Operations) > 0 {
			op := intf.Operations[0]
			if op.OperationID != "listUsers" {
				t.Errorf("Operation ID = %q, want %q", op.OperationID, "listUsers")
			}
			if op.Method != types.GET {
				t.Errorf("Operation method = %q, want %q", op.Method, types.GET)
			}
			if op.Path != "/users" {
				t.Errorf("Operation path = %q, want %q", op.Path, "/users")
			}
		}
	})

	t.Run("complex spec", func(t *testing.T) {
		intf := NewInterface(complexDoc)

		// Check tags were collected
		if len(intf.Tags) == 0 {
			t.Error("Interface should have tags")
		}

		// Check operations
		if len(intf.Operations) == 0 {
			t.Error("Interface should have operations")
		}

		// Find specific operations
		var getUserOp, createResourceOp *types.OperationInfo
		for i := range intf.Operations {
			if intf.Operations[i].OperationID == "getUser" {
				getUserOp = &intf.Operations[i]
			}
			if intf.Operations[i].OperationID == "createResource" {
				createResourceOp = &intf.Operations[i]
			}
		}

		if getUserOp == nil {
			t.Error("getUser operation not found")
		} else {
			// Check parameters
			hasUserIdParam := false
			hasIncludeDetailsParam := false
			for _, param := range getUserOp.Parameters {
				if param.Name == "userId" && param.In == "path" && param.Required {
					hasUserIdParam = true
				}
				if param.Name == "includeDetails" && param.In == "query" && !param.Required {
					hasIncludeDetailsParam = true
				}
			}
			if !hasUserIdParam {
				t.Error("getUser should have userId path parameter")
			}
			if !hasIncludeDetailsParam {
				t.Error("getUser should have includeDetails query parameter")
			}
		}

		if createResourceOp == nil {
			t.Error("createResource operation not found")
		} else {
			// Check request body
			if createResourceOp.RequestBody == nil {
				t.Error("createResource should have request body")
			} else {
				if !createResourceOp.RequestBody.Required {
					t.Error("createResource request body should be required")
				}
				// Should prefer merge-patch+json content type
				if !strings.Contains(createResourceOp.RequestBody.ContentType, "merge-patch+json") {
					t.Errorf("createResource should prefer merge-patch+json, got %q", createResourceOp.RequestBody.ContentType)
				}
			}
		}
	})
}


func TestCollectTags(t *testing.T) {
	doc, err := LoadDescription("testdata/complex.yaml")
	if err != nil {
		t.Fatalf("Failed to load spec: %v", err)
	}

	intf := NewInterface(doc)

	// Should have collected tags
	if len(intf.Tags) == 0 {
		t.Error("No tags collected")
	}

	// Check specific tags
	hasResources := false
	hasUsers := false
	for _, tag := range intf.Tags {
		if tag == "resources" {
			hasResources = true
		}
		if tag == "users" {
			hasUsers = true
		}
	}

	if !hasResources {
		t.Error("Missing 'resources' tag")
	}
	if !hasUsers {
		t.Error("Missing 'users' tag")
	}
}

func TestCollectOperations(t *testing.T) {
	doc, err := LoadDescription("testdata/complex.yaml")
	if err != nil {
		t.Fatalf("Failed to load spec: %v", err)
	}

	intf := &Interface{
		Doc:        doc,
		Operations: []types.OperationInfo{},
	}

	intf.collectOperations()

	// Count operations by method
	methodCounts := make(map[types.HTTPMethod]int)
	for _, op := range intf.Operations {
		methodCounts[op.Method]++
	}

	// Should have various HTTP methods
	if methodCounts[types.GET] < 2 {
		t.Errorf("Expected at least 2 GET operations, got %d", methodCounts[types.GET])
	}
	if methodCounts[types.POST] < 1 {
		t.Errorf("Expected at least 1 POST operation, got %d", methodCounts[types.POST])
	}
	if methodCounts[types.PUT] < 1 {
		t.Errorf("Expected at least 1 PUT operation, got %d", methodCounts[types.PUT])
	}
	if methodCounts[types.DELETE] < 1 {
		t.Errorf("Expected at least 1 DELETE operation, got %d", methodCounts[types.DELETE])
	}

	// Check operation details
	for _, op := range intf.Operations {
		if op.OperationID == "" {
			t.Error("Operation missing operationId")
		}
		if op.Path == "" {
			t.Error("Operation missing path")
		}
		if !types.IsValidMethod(string(op.Method)) {
			t.Errorf("Invalid HTTP method: %q", op.Method)
		}
	}
}

func TestInterfaceNameGeneration(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{
			title:    "Cloud Service API",
			expected: "cloud-service-api",
		},
		{
			title:    "IBM Watson API",
			expected: "ibm-watson-api",
		},
		{
			title:    "Simple API",
			expected: "simple-api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			// Create a minimal doc with specific title
			doc, _ := LoadDescription("testdata/minimal.yaml")
			doc.Info.Title = tt.title

			intf := NewInterface(doc)

			if intf.Name != tt.expected {
				t.Errorf("Interface name = %q, want %q", intf.Name, tt.expected)
			}
		})
	}
}

func TestRequestBodyContentTypePreference(t *testing.T) {
	doc, err := LoadDescription("testdata/complex.yaml")
	if err != nil {
		t.Fatalf("Failed to load spec: %v", err)
	}

	intf := NewInterface(doc)

	// Find createResource operation
	var createResourceOp *types.OperationInfo
	for i := range intf.Operations {
		if intf.Operations[i].OperationID == "createResource" {
			createResourceOp = &intf.Operations[i]
			break
		}
	}

	if createResourceOp == nil {
		t.Fatal("createResource operation not found")
	}

	if createResourceOp.RequestBody == nil {
		t.Fatal("createResource should have request body")
	}

	// Should prefer merge-patch+json over regular json
	if !strings.Contains(createResourceOp.RequestBody.ContentType, "merge-patch+json") {
		t.Errorf("Should prefer merge-patch+json content type, got %q", createResourceOp.RequestBody.ContentType)
	}
}
