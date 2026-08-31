package types

import "github.com/google/jsonschema-go/jsonschema"

// This file contains non-MCP types used throughout the application
// All MCP-related types should use the official SDK directly:
// import "github.com/modelcontextprotocol/go-sdk/mcp"

// HTTPMethod represents valid HTTP methods
type HTTPMethod string

const (
	GET     HTTPMethod = "GET"
	POST    HTTPMethod = "POST"
	PUT     HTTPMethod = "PUT"
	DELETE  HTTPMethod = "DELETE"
	PATCH   HTTPMethod = "PATCH"
	HEAD    HTTPMethod = "HEAD"
	OPTIONS HTTPMethod = "OPTIONS"
	TRACE   HTTPMethod = "TRACE"
)

// IsValidMethod checks if a string is a valid HTTP method
func IsValidMethod(method string) bool {
	switch HTTPMethod(method) {
	case GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, TRACE:
		return true
	default:
		return false
	}
}

// OperationInfo contains information about an OpenAPI operation
type OperationInfo struct {
	OperationID string
	Method      HTTPMethod
	Path        string
	Summary     string
	Description string
	Tags        []string
	Parameters  []ParameterInfo
	RequestBody *RequestBodyInfo
}

// ParameterInfo contains information about an OpenAPI parameter
type ParameterInfo struct {
	Name        string
	In          string // path, query, header, cookie
	Required    bool
	Schema      *jsonschema.Schema
	Description string
}

// RequestBodyInfo contains information about an OpenAPI request body
type RequestBodyInfo struct {
	Required    bool
	ContentType string
	Schema      *jsonschema.Schema
}

// ConfigOutput represents the configuration output for MCP clients
type ConfigOutput struct {
	MCPServers map[string]MCPClientServerConfig `json:"mcpServers"`
}

// MCPClientServerConfig represents an MCP client server configuration
type MCPClientServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}
