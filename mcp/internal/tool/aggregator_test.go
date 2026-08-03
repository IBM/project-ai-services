package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/project-ai-services/ai-services-mcp/internal/openapi"
	"github.com/project-ai-services/ai-services-mcp/internal/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Mock OpenAPI interface for testing
func createMockInterface() *openapi.Interface {
	// Create a simple Interface struct directly rather than building a complex Document
	operations := []types.OperationInfo{
		{
			OperationID: "getUser",
			Method:      types.GET,
			Path:        "/users/{id}",
			Description: "Get user by ID",
			Tags:        []string{"users"},
			Parameters: []types.ParameterInfo{
				{
					Name:     "id",
					In:       "path",
					Required: true,
					Schema:   &jsonschema.Schema{Type: "string"},
				},
			},
		},
		{
			OperationID: "listUsers",
			Method:      types.GET,
			Path:        "/users",
			Description: "List all users",
			Tags:        []string{"users"},
			Parameters: []types.ParameterInfo{
				{
					Name:     "limit",
					In:       "query",
					Required: false,
					Schema:   &jsonschema.Schema{Type: "integer"},
				},
			},
		},
		{
			OperationID: "createResource",
			Method:      types.POST,
			Path:        "/resources",
			Description: "Create a new resource",
			Tags:        []string{"resources"},
			RequestBody: &types.RequestBodyInfo{
				Required:    true,
				ContentType: "application/json",
				Schema:      &jsonschema.Schema{Type: "object"},
			},
		},
	}

	// Create a proper mock document using libopenapi structures
	info := &base.Info{
		Title:   "Test API",
		Version: "1.0.0",
	}

	// Create the root Document
	doc := &v3.Document{
		Version: "3.1.0",
		Info:    info,
	}

	return &openapi.Interface{
		Doc:        doc,
		Name:       "test-api",
		Operations: operations,
		Tags:       []string{"users", "resources"},
	}
}

func TestNewAggregator(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	tests := []struct {
		name          string
		intf          *openapi.Interface
		endpoint      string
		globalQuery   map[string]string
		globalHeaders map[string]string
		wantErr       bool
	}{
		{
			name:     "successful creation",
			intf:     intf,
			endpoint: "https://api.example.com",
			globalQuery: map[string]string{
				"version": "2023-10-01",
			},
			globalHeaders: map[string]string{
				"User-Agent": "test-client",
			},
			wantErr: false,
		},
		{
			name:     "with empty endpoint",
			intf:     intf,
			endpoint: "",
			wantErr:  false,
		},
		{
			name:     "with nil maps",
			intf:     intf,
			endpoint: "https://api.example.com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregator, err := NewAggregator(tt.intf, tt.endpoint, auth, tt.globalQuery, tt.globalHeaders)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewAggregator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if aggregator == nil {
					t.Fatal("NewAggregator() returned nil")
				}

				if aggregator.intf != tt.intf {
					t.Error("Aggregator interface not set correctly")
				}

				if aggregator.endpoint != tt.endpoint {
					t.Errorf("Aggregator endpoint = %q, want %q", aggregator.endpoint, tt.endpoint)
				}

				if len(aggregator.providers) != len(tt.intf.Operations) {
					t.Errorf("Aggregator providers count = %d, want %d", len(aggregator.providers), len(tt.intf.Operations))
				}

				// Check that global headers are canonicalized (lowercase)
				for key := range aggregator.globalHeaders {
					if key != strings.ToLower(key) {
						t.Errorf("Global header key %q should be lowercase", key)
					}
				}
			}
		})
	}
}

func TestAggregator_GetTools(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	tests := []struct {
		name          string
		tags          []string
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "get all tools",
			tags:          nil,
			expectedCount: 3,
			expectedNames: []string{"getUser", "listUsers", "createResource"},
		},
		{
			name:          "filter by users tag",
			tags:          []string{"users"},
			expectedCount: 2,
			expectedNames: []string{"getUser", "listUsers"},
		},
		{
			name:          "filter by resources tag",
			tags:          []string{"resources"},
			expectedCount: 1,
			expectedNames: []string{"createResource"},
		},
		{
			name:          "filter by multiple tags",
			tags:          []string{"users", "resources"},
			expectedCount: 3,
			expectedNames: []string{"getUser", "listUsers", "createResource"},
		},
		{
			name:          "filter by comma-separated tags",
			tags:          []string{"users,resources"},
			expectedCount: 3,
			expectedNames: []string{"getUser", "listUsers", "createResource"},
		},
		{
			name:          "filter by non-existent tag",
			tags:          []string{"nonexistent"},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:          "filter with spaces in tag list",
			tags:          []string{" users , resources "},
			expectedCount: 3,
			expectedNames: []string{"getUser", "listUsers", "createResource"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := aggregator.GetTools(tt.tags)

			if len(tools) != tt.expectedCount {
				t.Errorf("GetTools() count = %d, want %d", len(tools), tt.expectedCount)
				return
			}

			// Check that expected tools are present
			toolNames := make(map[string]bool)
			for _, tool := range tools {
				toolNames[tool.Name] = true
			}

			for _, expectedName := range tt.expectedNames {
				if !toolNames[expectedName] {
					t.Errorf("Expected tool %q not found in results", expectedName)
				}
			}

			// Verify tool structure
			for _, tool := range tools {
				if tool.Name == "" {
					t.Error("Tool name should not be empty")
				}
				if tool.InputSchema == nil {
					t.Errorf("Tool %q should have input schema", tool.Name)
				}
			}
		})
	}
}

func TestAggregator_HandleToolCall(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	tests := []struct {
		name     string
		toolName string
		params   json.RawMessage
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "existing tool",
			toolName: "getUser",
			params:   json.RawMessage(`{"id": "123"}`),
			wantErr:  true, // Will fail due to network request, but should find the tool
		},
		{
			name:     "non-existent tool",
			toolName: "nonExistentTool",
			params:   json.RawMessage(`{}`),
			wantErr:  true,
			errMsg:   "unknown tool",
		},
		{
			name:     "empty tool name",
			toolName: "",
			params:   json.RawMessage(`{}`),
			wantErr:  true,
			errMsg:   "unknown tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mcpParams := &mcp.CallToolParamsRaw{
				Name:      tt.toolName,
				Arguments: tt.params,
			}

			result, err := aggregator.HandleToolCall(ctx, mcpParams)

			if (err != nil) != tt.wantErr {
				t.Errorf("HandleToolCall() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("HandleToolCall() error = %v, want error containing %q", err, tt.errMsg)
				}
			}

			if !tt.wantErr && result == nil {
				t.Error("HandleToolCall() should return result for successful calls")
			}
		})
	}
}

func TestAggregator_GetFriendlyName(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	friendlyName := aggregator.GetFriendlyName()
	expected := "Test API"

	if friendlyName != expected {
		t.Errorf("GetFriendlyName() = %q, want %q", friendlyName, expected)
	}
}

func TestAggregator_GetName(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	name := aggregator.GetName()
	expected := "test-api"

	if name != expected {
		t.Errorf("GetName() = %q, want %q", name, expected)
	}
}

func TestAggregator_GetTags(t *testing.T) {
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	tags := aggregator.GetTags()
	expectedTags := []string{"users", "resources"}

	if len(tags) != len(expectedTags) {
		t.Errorf("GetTags() count = %d, want %d", len(tags), len(expectedTags))
		return
	}

	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	for _, expectedTag := range expectedTags {
		if !tagSet[expectedTag] {
			t.Errorf("Expected tag %q not found in results", expectedTag)
		}
	}
}

func TestCanonicalizeHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: map[string]string{},
		},
		{
			name:     "empty input",
			input:    map[string]string{},
			expected: map[string]string{},
		},
		{
			name: "mixed case headers",
			input: map[string]string{
				"Content-Type":    "application/json",
				"User-Agent":      "test-client",
				"X-Custom-Header": "custom-value",
			},
			expected: map[string]string{
				"content-type":    "application/json",
				"user-agent":      "test-client",
				"x-custom-header": "custom-value",
			},
		},
		{
			name: "already lowercase",
			input: map[string]string{
				"content-type": "application/json",
				"user-agent":   "test-client",
			},
			expected: map[string]string{
				"content-type": "application/json",
				"user-agent":   "test-client",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeHeaders(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("canonicalizeHeaders() count = %d, want %d", len(result), len(tt.expected))
				return
			}

			for key, expectedValue := range tt.expected {
				if actualValue, exists := result[key]; !exists {
					t.Errorf("Expected key %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("canonicalizeHeaders()[%q] = %q, want %q", key, actualValue, expectedValue)
				}
			}
		})
	}
}

func TestAggregator_Integration(t *testing.T) {
	// Integration test combining multiple operations
	intf := createMockInterface()
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, err := NewAggregator(intf, "https://api.example.com", auth,
		map[string]string{"version": "2023-10-01"},
		map[string]string{"User-Agent": "test-client"})
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	// Test that all operations are available
	allTools := aggregator.GetTools(nil)
	if len(allTools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(allTools))
	}

	// Test tag filtering
	userTools := aggregator.GetTools([]string{"users"})
	if len(userTools) != 2 {
		t.Errorf("Expected 2 user tools, got %d", len(userTools))
	}

	resourceTools := aggregator.GetTools([]string{"resources"})
	if len(resourceTools) != 1 {
		t.Errorf("Expected 1 resource tool, got %d", len(resourceTools))
	}

	// Test metadata methods (skip GetFriendlyName as it requires complex mocking)

	if aggregator.GetName() != "test-api" {
		t.Errorf("Unexpected name: %q", aggregator.GetName())
	}

	tags := aggregator.GetTags()
	if len(tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(tags))
	}
}
