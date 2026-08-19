package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/project-ai-services/mcp/internal/errors"
)

func TestValidateEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid IBM Cloud endpoint",
			endpoint: "https://resource-controller.cloud.ibm.com",
			wantErr:  false,
		},
		{
			name:     "valid IBM Cloud endpoint with region",
			endpoint: "https://us-south.resource-controller.cloud.ibm.com",
			wantErr:  false,
		},
		{
			name:     "http instead of https",
			endpoint: "http://resource-controller.cloud.ibm.com",
			wantErr:  true,
			errMsg:   "Must use HTTPS protocol",
		},
		{
			name:     "non-IBM Cloud domain",
			endpoint: "https://api.example.com",
			wantErr:  false,
		},
		{
			name:     "invalid URL format",
			endpoint: "not-a-url",
			wantErr:  true,
			errMsg:   "Must use HTTPS protocol",
		},
		{
			name:     "too short domain",
			endpoint: "https://short.com",
			wantErr:  false,
		},
		{
			name:     "valid subdomain",
			endpoint: "https://iam.cloud.ibm.com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.endpoint)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateEndpoint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateEndpoint() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestCreateAuthenticator(t *testing.T) {
	// Save original values
	origAuthCLI := authCLI
	origAuthAPIKey := authAPIKey
	origAuthToken := authToken
	origAuthPassthrough := authPassthrough
	origHTTPMode := httpMode

	defer func() {
		// Restore original values
		authCLI = origAuthCLI
		authAPIKey = origAuthAPIKey
		authToken = origAuthToken
		authPassthrough = origAuthPassthrough
		httpMode = origHTTPMode
	}()

	tests := []struct {
		name         string
		setupFlags   func()
		wantErr      bool
		errMsg       string
		expectedType string
	}{
		{
			name: "no authentication provided",
			setupFlags: func() {
				authCLI = false
				authAPIKey = ""
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr: true,
			errMsg:  "Must provide an authentication option",
		},
		{
			name: "multiple authentication options",
			setupFlags: func() {
				authCLI = true
				authAPIKey = "test-key"
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr: true,
			errMsg:  "Must not use more than one authentication option",
		},
		{
			name: "CLI authentication",
			setupFlags: func() {
				authCLI = true
				authAPIKey = ""
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr:      false,
			expectedType: "cli",
		},
		{
			name: "API key authentication",
			setupFlags: func() {
				authCLI = false
				authAPIKey = "test-api-key"
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr:      false,
			expectedType: "api-key",
		},
		{
			name: "Environment variable authentication",
			setupFlags: func() {
				authCLI = false
				authAPIKey = "$TEST_API_KEY"
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr: true, // Will fail because TEST_API_KEY is not set in test environment
			errMsg:  "Environment variable TEST_API_KEY is not set or empty",
		},
		{
			name: "1Password authentication",
			setupFlags: func() {
				authCLI = false
				authAPIKey = "op://vault/item/field"
				authToken = ""
				authPassthrough = false
				httpMode = false
			},
			wantErr:      false,
			expectedType: "1password",
		},
		{
			name: "Token authentication",
			setupFlags: func() {
				authCLI = false
				authAPIKey = ""
				authToken = "test-token"
				authPassthrough = false
				httpMode = false
			},
			wantErr:      false,
			expectedType: "token",
		},
		{
			name: "Passthrough authentication",
			setupFlags: func() {
				authCLI = false
				authAPIKey = ""
				authToken = ""
				authPassthrough = true
				httpMode = true
			},
			wantErr:      false,
			expectedType: "passthrough",
		},
		{
			name: "API key authentication in HTTP mode",
			setupFlags: func() {
				authCLI = false
				authAPIKey = "test-api-key"
				authToken = ""
				authPassthrough = false
				httpMode = true
			},
			wantErr: true,
			errMsg:  "Must use --auth-passthrough with --http",
		},
		{
			name: "CLI authentication in HTTP mode",
			setupFlags: func() {
				authCLI = true
				authAPIKey = ""
				authToken = ""
				authPassthrough = false
				httpMode = true
			},
			wantErr: true,
			errMsg:  "Must use --auth-passthrough with --http",
		},
		{
			name: "Token authentication in HTTP mode",
			setupFlags: func() {
				authCLI = false
				authAPIKey = ""
				authToken = "test-token"
				authPassthrough = false
				httpMode = true
			},
			wantErr: true,
			errMsg:  "Must use --auth-passthrough with --http",
		},
		{
			name: "Passthrough authentication in stdio mode",
			setupFlags: func() {
				authCLI = false
				authAPIKey = ""
				authToken = ""
				authPassthrough = true
				httpMode = false
			},
			wantErr: true,
			errMsg:  "Must use --http with --auth-passthrough",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupFlags()

			auth, err := createAuthenticator()

			if (err != nil) != tt.wantErr {
				t.Errorf("createAuthenticator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("createAuthenticator() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if !tt.wantErr {
				if auth == nil {
					t.Error("createAuthenticator() returned nil authenticator")
					return
				}

				if auth.GetType() != tt.expectedType {
					t.Errorf("createAuthenticator() type = %s, want %s", auth.GetType(), tt.expectedType)
				}
			}
		})
	}
}

func TestParseKeyValuePairs(t *testing.T) {
	tests := []struct {
		name     string
		pairs    []string
		pairType string
		want     map[string]string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "empty pairs",
			pairs:    []string{},
			pairType: "test",
			want:     map[string]string{},
			wantErr:  false,
		},
		{
			name:     "single valid pair",
			pairs:    []string{"key=value"},
			pairType: "test",
			want:     map[string]string{"key": "value"},
			wantErr:  false,
		},
		{
			name:     "multiple valid pairs",
			pairs:    []string{"key1=value1", "key2=value2"},
			pairType: "test",
			want:     map[string]string{"key1": "value1", "key2": "value2"},
			wantErr:  false,
		},
		{
			name:     "pair with spaces",
			pairs:    []string{" key = value "},
			pairType: "test",
			want:     map[string]string{"key": "value"},
			wantErr:  false,
		},
		{
			name:     "pair with multiple equals",
			pairs:    []string{"key=value=with=equals"},
			pairType: "test",
			want:     map[string]string{"key": "value=with=equals"},
			wantErr:  false,
		},
		{
			name:     "invalid pair format",
			pairs:    []string{"invalidpair"},
			pairType: "header",
			want:     nil,
			wantErr:  true,
			errMsg:   "Must provide header value in the form: <name>=<value>",
		},
		{
			name:     "empty key",
			pairs:    []string{"=value"},
			pairType: "test",
			want:     map[string]string{"": "value"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseKeyValuePairs(tt.pairs, tt.pairType)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseKeyValuePairs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("parseKeyValuePairs() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if !tt.wantErr {
				if len(result) != len(tt.want) {
					t.Errorf("parseKeyValuePairs() result length = %d, want %d", len(result), len(tt.want))
					return
				}

				for key, expectedValue := range tt.want {
					if actualValue, exists := result[key]; !exists {
						t.Errorf("parseKeyValuePairs() missing key %q", key)
					} else if actualValue != expectedValue {
						t.Errorf("parseKeyValuePairs()[%q] = %q, want %q", key, actualValue, expectedValue)
					}
				}
			}
		})
	}
}

func TestValidateTags(t *testing.T) {
	availableTags := []string{"users", "resources", "admin"}

	tests := []struct {
		name          string
		requestedTags []string
		wantErr       bool
		errMsg        string
	}{
		{
			name:          "valid single tag",
			requestedTags: []string{"users"},
			wantErr:       false,
		},
		{
			name:          "valid multiple tags",
			requestedTags: []string{"users", "resources"},
			wantErr:       false,
		},
		{
			name:          "valid comma-separated tags",
			requestedTags: []string{"users,resources"},
			wantErr:       false,
		},
		{
			name:          "valid tags with spaces",
			requestedTags: []string{" users , resources "},
			wantErr:       false,
		},
		{
			name:          "invalid tag",
			requestedTags: []string{"unknown"},
			wantErr:       true,
			errMsg:        "tag(s) not found: unknown",
		},
		{
			name:          "mixed valid and invalid tags",
			requestedTags: []string{"users", "unknown"},
			wantErr:       true,
			errMsg:        "tag(s) not found: unknown",
		},
		{
			name:          "multiple invalid tags",
			requestedTags: []string{"unknown1", "unknown2"},
			wantErr:       true,
			errMsg:        "tag(s) not found: unknown1, unknown2",
		},
		{
			name:          "empty tag list",
			requestedTags: []string{},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTags(tt.requestedTags, availableTags)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateTags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateTags() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestHandleError(t *testing.T) {
	// Test with regular error
	regularErr := fmt.Errorf("regular error")

	// This function prints to stderr, so we can't easily test the output
	// but we can verify it doesn't panic
	t.Run("regular error", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleError() panicked with regular error: %v", r)
			}
		}()
		handleError(regularErr)
	})

	// Test with usage error
	usageErr := errors.NewUsageError("test usage error")

	t.Run("usage error", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleError() panicked with usage error: %v", r)
			}
		}()
		handleError(usageErr)
	})
}

func TestGetUsage(t *testing.T) {
	usage := getUsage()

	if usage == "" {
		t.Error("getUsage() should return non-empty string")
	}

	// Check for key sections
	expectedSections := []string{
		"Usage:",
		"Flags:",
		"--description",
		"--endpoint",
		"--auth-api-key",
		"--auth-cli",
		"Transport Modes:",
	}

	for _, section := range expectedSections {
		if !strings.Contains(usage, section) {
			t.Errorf("getUsage() missing section: %s", section)
		}
	}
}

// Mock test for functions that require complex setup
func TestMainFunctionExists(t *testing.T) {
	// Test that main function exists (can't test execution easily)
	// We can't easily test main() execution as it would try to parse flags
	// and execute the actual application
	t.Log("main function exists and is part of the application entry point")
}
