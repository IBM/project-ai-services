package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/project-ai-services/mcp/internal/authenticator"
	"github.com/project-ai-services/mcp/internal/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Mock authenticator for testing
type mockAuthenticator struct {
	token       string
	err         error
	passthrough bool
	authType    string
}

func (m *mockAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	return m.token, m.err
}

func (m *mockAuthenticator) IsPassthrough() bool {
	return m.passthrough
}

func (m *mockAuthenticator) GetType() string {
	return m.authType
}

func TestNewProvider(t *testing.T) {
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	tests := []struct {
		name          string
		operation     types.OperationInfo
		endpoint      string
		globalQuery   map[string]string
		globalHeaders map[string]string
		wantErr       bool
	}{
		{
			name: "simple GET operation",
			operation: types.OperationInfo{
				OperationID: "getUser",
				Method:      types.GET,
				Path:        "/users/{id}",
				Description: "Get user by ID",
				Parameters: []types.ParameterInfo{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema:   &jsonschema.Schema{Type: "string"},
					},
				},
			},
			endpoint: "https://api.example.com",
			wantErr:  false,
		},
		{
			name: "POST operation with body",
			operation: types.OperationInfo{
				OperationID: "createUser",
				Method:      types.POST,
				Path:        "/users",
				Description: "Create a new user",
				RequestBody: &types.RequestBodyInfo{
					Required:    true,
					ContentType: "application/json",
					Schema:      &jsonschema.Schema{Type: "object"},
				},
			},
			endpoint: "https://api.example.com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.operation, tt.endpoint, auth, tt.globalQuery, tt.globalHeaders, false)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if provider == nil {
					t.Fatal("NewProvider() returned nil provider")
				}

				if provider.operation.OperationID != tt.operation.OperationID {
					t.Errorf("Provider operation ID = %q, want %q", provider.operation.OperationID, tt.operation.OperationID)
				}

				if provider.endpoint != tt.endpoint {
					t.Errorf("Provider endpoint = %q, want %q", provider.endpoint, tt.endpoint)
				}

				if provider.inputSchema == nil {
					t.Error("Provider inputSchema should not be nil")
				}
			}
		})
	}
}

func TestProvider_GetTool(t *testing.T) {
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	operation := types.OperationInfo{
		OperationID: "testOperation",
		Method:      types.GET,
		Path:        "/test",
		Description: "Test operation",
	}

	provider, err := NewProvider(operation, "https://api.example.com", auth, nil, nil, false)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	tool := provider.GetTool()

	if tool == nil {
		t.Fatal("GetTool() returned nil")
	}

	if tool.Name != operation.OperationID {
		t.Errorf("Tool name = %q, want %q", tool.Name, operation.OperationID)
	}

	if tool.Description != operation.Description {
		t.Errorf("Tool description = %q, want %q", tool.Description, operation.Description)
	}

	if tool.InputSchema == nil {
		t.Error("Tool InputSchema should not be nil")
	}
}

func TestGetBodyName(t *testing.T) {
	tests := []struct {
		name      string
		operation types.OperationInfo
		expected  string
	}{
		{
			name: "create operation",
			operation: types.OperationInfo{
				OperationID: "create_user",
				Method:      types.POST,
			},
			expected: "prototype",
		},
		{
			name: "replace operation",
			operation: types.OperationInfo{
				OperationID: "replace_user",
				Method:      types.PUT,
			},
			expected: "prototype",
		},
		{
			name: "PATCH operation",
			operation: types.OperationInfo{
				OperationID: "updateUser",
				Method:      types.PATCH,
			},
			expected: "patch",
		},
		{
			name: "other operation",
			operation: types.OperationInfo{
				OperationID: "updateUser",
				Method:      types.PUT,
			},
			expected: "data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBodyName(tt.operation)
			if result != tt.expected {
				t.Errorf("getBodyName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestProvider_buildInputSchema(t *testing.T) {
	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	tests := []struct {
		name         string
		operation    types.OperationInfo
		endpoint     string
		expectFields []string
	}{
		{
			name: "path parameter only",
			operation: types.OperationInfo{
				OperationID: "getUser",
				Method:      types.GET,
				Path:        "/users/{id}",
				Parameters: []types.ParameterInfo{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema:   &jsonschema.Schema{Type: "string"},
					},
				},
			},
			endpoint:     "https://api.example.com",
			expectFields: []string{"id"},
		},
		{
			name: "query parameters",
			operation: types.OperationInfo{
				OperationID: "listUsers",
				Method:      types.GET,
				Path:        "/users",
				Parameters: []types.ParameterInfo{
					{
						Name:     "limit",
						In:       "query",
						Required: false,
						Schema:   &jsonschema.Schema{Type: "integer"},
					},
					{
						Name:     "offset",
						In:       "query",
						Required: false,
						Schema:   &jsonschema.Schema{Type: "integer"},
					},
				},
			},
			endpoint:     "https://api.example.com",
			expectFields: []string{"query"},
		},
		{
			name: "header parameters",
			operation: types.OperationInfo{
				OperationID: "getUser",
				Method:      types.GET,
				Path:        "/users/{id}",
				Parameters: []types.ParameterInfo{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema:   &jsonschema.Schema{Type: "string"},
					},
					{
						Name:     "X-Custom-Header",
						In:       "header",
						Required: true,
						Schema:   &jsonschema.Schema{Type: "string"},
					},
				},
			},
			endpoint:     "https://api.example.com",
			expectFields: []string{"id", "headers"},
		},
		{
			name: "request body",
			operation: types.OperationInfo{
				OperationID: "createUser",
				Method:      types.POST,
				Path:        "/users",
				RequestBody: &types.RequestBodyInfo{
					Required:    true,
					ContentType: "application/json",
					Schema:      &jsonschema.Schema{Type: "object"},
				},
			},
			endpoint:     "https://api.example.com",
			expectFields: []string{"data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.operation, tt.endpoint, auth, nil, nil, false)
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}

			schema := provider.inputSchema
			if schema == nil {
				t.Fatal("inputSchema is nil")
			}

			if schema.Type != "object" {
				t.Errorf("Schema type = %q, want %q", schema.Type, "object")
			}

			for _, field := range tt.expectFields {
				if _, exists := schema.Properties[field]; !exists {
					t.Errorf("Expected field %q not found in schema properties", field)
				}
			}
		})
	}
}

func TestProvider_buildRequestURL(t *testing.T) {
	tests := []struct {
		name     string
		provider *Provider
		params   json.RawMessage
		expected string
		wantErr  bool
	}{
		{
			name: "simple path with endpoint",
			provider: &Provider{
				operation: types.OperationInfo{
					Path: "/users",
				},
				endpoint: "https://api.example.com",
			},
			params:   json.RawMessage(`{}`),
			expected: "https://api.example.com/users",
			wantErr:  false,
		},
		{
			name: "path with parameter",
			provider: &Provider{
				operation: types.OperationInfo{
					Path: "/users/{id}",
				},
				endpoint: "https://api.example.com",
			},
			params:   json.RawMessage(`{"id": "123"}`),
			expected: "https://api.example.com/users/123",
			wantErr:  false,
		},
		{
			name: "path with query parameters",
			provider: &Provider{
				operation: types.OperationInfo{
					Path: "/users",
				},
				endpoint: "https://api.example.com",
			},
			params:   json.RawMessage(`{"query":{"limit":"10","offset":"0"}}`),
			expected: "https://api.example.com/users?limit=10&offset=0",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create MCP params
			mcpParams := &mcp.CallToolParamsRaw{
				Name:      "test",
				Arguments: tt.params,
			}

			url, err := tt.provider.buildRequestURL(mcpParams)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildRequestURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// For query parameters, we need to check if the URL contains the expected parts
				// since the order of query parameters can vary
				if strings.Contains(tt.expected, "?") {
					expectedParts := strings.Split(tt.expected, "?")
					urlParts := strings.Split(url, "?")

					if len(urlParts) != 2 {
						t.Errorf("buildRequestURL() = %q, expected query parameters", url)
						return
					}

					if urlParts[0] != expectedParts[0] {
						t.Errorf("buildRequestURL() base = %q, want %q", urlParts[0], expectedParts[0])
					}

					// Check that query parameters are present (order may vary)
					expectedQuery := expectedParts[1]
					actualQuery := urlParts[1]

					if !strings.Contains(actualQuery, "limit=10") && strings.Contains(expectedQuery, "limit=10") {
						t.Errorf("buildRequestURL() missing limit parameter")
					}

					if !strings.Contains(actualQuery, "offset=0") && strings.Contains(expectedQuery, "offset=0") {
						t.Errorf("buildRequestURL() missing offset parameter")
					}
				} else {
					if url != tt.expected {
						t.Errorf("buildRequestURL() = %q, want %q", url, tt.expected)
					}
				}
			}
		})
	}
}

func TestProvider_buildHeaders(t *testing.T) {
	tests := []struct {
		name            string
		authenticator   authenticator.Authenticator
		params          json.RawMessage
		globalHeaders   map[string]string
		expectedHeaders []string
		wantErr         bool
	}{
		{
			name: "regular authentication",
			authenticator: &mockAuthenticator{
				token:       "test-token-123",
				passthrough: false,
				authType:    "api-key",
			},
			params:          json.RawMessage(`{}`),
			expectedHeaders: []string{"authorization"},
			wantErr:         false,
		},
		{
			name: "passthrough authentication with context",
			authenticator: &mockAuthenticator{
				passthrough: true,
				authType:    "passthrough",
			},
			params:  json.RawMessage(`{}`),
			wantErr: true, // Will fail without proper context
		},
		{
			name: "custom headers",
			authenticator: &mockAuthenticator{
				token:       "test-token",
				passthrough: false,
				authType:    "api-key",
			},
			params:          json.RawMessage(`{"headers":{"X-Custom-Header":"custom-value"}}`),
			expectedHeaders: []string{"authorization", "x-custom-header"},
			wantErr:         false,
		},
		{
			name: "global headers",
			authenticator: &mockAuthenticator{
				token:       "test-token",
				passthrough: false,
				authType:    "api-key",
			},
			params: json.RawMessage(`{}`),
			globalHeaders: map[string]string{
				"x-global-header": "global-value",
			},
			expectedHeaders: []string{"authorization", "x-global-header"},
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{
				authenticator: tt.authenticator,
				globalHeaders: tt.globalHeaders,
			}

			ctx := context.Background()
			mcpParams := &mcp.CallToolParamsRaw{
				Name:      "test",
				Arguments: tt.params,
			}

			headers, err := provider.buildHeaders(ctx, mcpParams)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildHeaders() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, expectedHeader := range tt.expectedHeaders {
					if _, exists := headers[expectedHeader]; !exists {
						t.Errorf("Expected header %q not found in headers", expectedHeader)
					}
				}

				// Check authorization header format for non-passthrough
				if !tt.authenticator.IsPassthrough() {
					if authHeader, exists := headers["authorization"]; exists {
						if !strings.HasPrefix(authHeader, "Bearer ") {
							t.Errorf("Authorization header should start with 'Bearer ', got %q", authHeader)
						}
					}
				}
			}
		})
	}
}

func TestProvider_Execute(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/123":
			if r.Method != "GET" {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": "123", "name": "Test User"}`))
		case "/users":
			if r.Method != "POST" {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id": "456", "name": "New User"}`))
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	auth := &mockAuthenticator{
		token:       "test-token-123",
		passthrough: false,
		authType:    "api-key",
	}

	tests := []struct {
		name          string
		operation     types.OperationInfo
		params        json.RawMessage
		wantErr       bool
		checkResponse func(t *testing.T, result *mcp.CallToolResult)
	}{
		{
			name: "successful GET request",
			operation: types.OperationInfo{
				OperationID: "getUser",
				Method:      types.GET,
				Path:        "/users/{id}",
				Parameters: []types.ParameterInfo{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema:   &jsonschema.Schema{Type: "string"},
					},
				},
			},
			params:  json.RawMessage(`{"id": "123"}`),
			wantErr: false,
			checkResponse: func(t *testing.T, result *mcp.CallToolResult) {
				if len(result.Content) != 1 {
					t.Errorf("Expected 1 content item, got %d", len(result.Content))
					return
				}

				textContent, ok := result.Content[0].(*mcp.TextContent)
				if !ok {
					t.Errorf("Expected TextContent, got %T", result.Content[0])
					return
				}

				if !strings.Contains(textContent.Text, "Test User") {
					t.Errorf("Response should contain 'Test User', got %q", textContent.Text)
				}
			},
		},
		{
			name: "successful POST request with body",
			operation: types.OperationInfo{
				OperationID: "createUser",
				Method:      types.POST,
				Path:        "/users",
				RequestBody: &types.RequestBodyInfo{
					Required:    true,
					ContentType: "application/json",
					Schema:      &jsonschema.Schema{Type: "object"},
				},
			},
			params:  json.RawMessage(`{"data":{"name":"New User"}}`),
			wantErr: false,
			checkResponse: func(t *testing.T, result *mcp.CallToolResult) {
				if len(result.Content) != 1 {
					t.Errorf("Expected 1 content item, got %d", len(result.Content))
					return
				}

				textContent, ok := result.Content[0].(*mcp.TextContent)
				if !ok {
					t.Errorf("Expected TextContent, got %T", result.Content[0])
					return
				}

				if !strings.Contains(textContent.Text, "New User") {
					t.Errorf("Response should contain 'New User', got %q", textContent.Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.operation, server.URL, auth, nil, nil, false)
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}

			ctx := context.Background()
			mcpParams := &mcp.CallToolParamsRaw{
				Name:      tt.operation.OperationID,
				Arguments: tt.params,
			}

			result, err := provider.Execute(ctx, mcpParams)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkResponse != nil {
				tt.checkResponse(t, result)
			}
		})
	}
}

func TestProvider_ExecuteTLSSkipVerify(t *testing.T) {
	// TLS test server presents a self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "123", "name": "Test User"}`))
	}))
	defer server.Close()

	auth := &mockAuthenticator{
		token:       "test-token-123",
		passthrough: false,
		authType:    "api-key",
	}

	operation := types.OperationInfo{
		OperationID: "getUser",
		Method:      types.GET,
		Path:        "/users/{id}",
		Parameters: []types.ParameterInfo{
			{
				Name:     "id",
				In:       "path",
				Required: true,
				Schema:   &jsonschema.Schema{Type: "string"},
			},
		},
	}
	mcpParams := &mcp.CallToolParamsRaw{
		Name:      operation.OperationID,
		Arguments: json.RawMessage(`{"id": "123"}`),
	}

	// Without skip, the self-signed certificate must be rejected
	provider, err := NewProvider(operation, server.URL, auth, nil, nil, false)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, err := provider.Execute(context.Background(), mcpParams); err == nil {
		t.Error("Execute() should fail against a self-signed certificate when tlsSkipVerify is false")
	}

	// With skip, the request succeeds
	provider, err = NewProvider(operation, server.URL, auth, nil, nil, true)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	result, err := provider.Execute(context.Background(), mcpParams)
	if err != nil {
		t.Fatalf("Execute() with tlsSkipVerify failed: %v", err)
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(textContent.Text, "Test User") {
		t.Errorf("Execute() with tlsSkipVerify returned unexpected content: %+v", result.Content)
	}
}
