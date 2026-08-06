package types

import (
	"testing"
)

func TestIsValidMethod(t *testing.T) {
	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{
			name:   "GET is valid",
			method: "GET",
			want:   true,
		},
		{
			name:   "POST is valid",
			method: "POST",
			want:   true,
		},
		{
			name:   "PUT is valid",
			method: "PUT",
			want:   true,
		},
		{
			name:   "DELETE is valid",
			method: "DELETE",
			want:   true,
		},
		{
			name:   "PATCH is valid",
			method: "PATCH",
			want:   true,
		},
		{
			name:   "HEAD is valid",
			method: "HEAD",
			want:   true,
		},
		{
			name:   "OPTIONS is valid",
			method: "OPTIONS",
			want:   true,
		},
		{
			name:   "TRACE is valid",
			method: "TRACE",
			want:   true,
		},
		{
			name:   "lowercase get is valid",
			method: "get",
			want:   false,
		},
		{
			name:   "CONNECT is invalid",
			method: "CONNECT",
			want:   false,
		},
		{
			name:   "empty string is invalid",
			method: "",
			want:   false,
		},
		{
			name:   "random string is invalid",
			method: "RANDOM",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidMethod(tt.method); got != tt.want {
				t.Errorf("IsValidMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestHTTPMethodConstants(t *testing.T) {
	tests := []struct {
		name     string
		method   HTTPMethod
		expected string
	}{
		{"GET constant", GET, "GET"},
		{"POST constant", POST, "POST"},
		{"PUT constant", PUT, "PUT"},
		{"DELETE constant", DELETE, "DELETE"},
		{"PATCH constant", PATCH, "PATCH"},
		{"HEAD constant", HEAD, "HEAD"},
		{"OPTIONS constant", OPTIONS, "OPTIONS"},
		{"TRACE constant", TRACE, "TRACE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.method) != tt.expected {
				t.Errorf("HTTPMethod constant %s = %q, want %q", tt.name, tt.method, tt.expected)
			}
		})
	}
}

func TestRegionServer(t *testing.T) {
	rs := RegionServer{
		Region: "us-south",
		URL:    "https://api.us-south.example.com",
	}

	if rs.Region != "us-south" {
		t.Errorf("RegionServer.Region = %q, want %q", rs.Region, "us-south")
	}

	if rs.URL != "https://api.us-south.example.com" {
		t.Errorf("RegionServer.URL = %q, want %q", rs.URL, "https://api.us-south.example.com")
	}
}

func TestConfigOutput(t *testing.T) {
	config := ConfigOutput{
		MCPServers: map[string]MCPClientServerConfig{
			"test-server": {
				Command: "test-command",
				Args:    []string{"arg1", "arg2"},
			},
		},
	}

	if len(config.MCPServers) != 1 {
		t.Errorf("ConfigOutput.MCPServers length = %d, want 1", len(config.MCPServers))
	}

	server, ok := config.MCPServers["test-server"]
	if !ok {
		t.Fatal("Expected server 'test-server' not found in MCPServers")
	}

	if server.Command != "test-command" {
		t.Errorf("MCPClientServerConfig.Command = %q, want %q", server.Command, "test-command")
	}

	if len(server.Args) != 2 || server.Args[0] != "arg1" || server.Args[1] != "arg2" {
		t.Errorf("MCPClientServerConfig.Args = %v, want [arg1 arg2]", server.Args)
	}
}

func TestOperationInfo(t *testing.T) {
	op := OperationInfo{
		OperationID: "getUser",
		Method:      GET,
		Path:        "/users/{id}",
		Summary:     "Get user by ID",
		Description: "Returns a single user",
		Tags:        []string{"users", "read"},
		Parameters: []ParameterInfo{
			{
				Name:        "id",
				In:          "path",
				Required:    true,
				Description: "User ID",
			},
		},
	}

	if op.OperationID != "getUser" {
		t.Errorf("OperationInfo.OperationID = %q, want %q", op.OperationID, "getUser")
	}

	if op.Method != GET {
		t.Errorf("OperationInfo.Method = %q, want %q", op.Method, GET)
	}

	if op.Path != "/users/{id}" {
		t.Errorf("OperationInfo.Path = %q, want %q", op.Path, "/users/{id}")
	}

	if op.Summary != "Get user by ID" {
		t.Errorf("OperationInfo.Summary = %q, want %q", op.Summary, "Get user by ID")
	}

	if op.Description != "Returns a single user" {
		t.Errorf("OperationInfo.Description = %q, want %q", op.Description, "Returns a single user")
	}

	if len(op.Tags) != 2 {
		t.Errorf("OperationInfo.Tags length = %d, want 2", len(op.Tags))
	}

	if len(op.Parameters) != 1 {
		t.Errorf("OperationInfo.Parameters length = %d, want 1", len(op.Parameters))
	}

	param := op.Parameters[0]
	if param.Name != "id" || param.In != "path" || !param.Required {
		t.Errorf("ParameterInfo incorrect: got %+v", param)
	}
}

func TestRequestBodyInfo(t *testing.T) {
	rb := RequestBodyInfo{
		Required:    true,
		ContentType: "application/json",
		Schema:      nil,
	}

	if !rb.Required {
		t.Error("RequestBodyInfo.Required = false, want true")
	}

	if rb.ContentType != "application/json" {
		t.Errorf("RequestBodyInfo.ContentType = %q, want %q", rb.ContentType, "application/json")
	}

	if rb.Schema != nil {
		t.Error("RequestBodyInfo.Schema should be nil")
	}
}
