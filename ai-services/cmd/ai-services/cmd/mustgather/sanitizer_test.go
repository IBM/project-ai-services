package mustgather

import (
	"strings"
	"testing"
)

func TestSecretSanitizer_SanitizeEnvVars(t *testing.T) {
	sanitizer := NewSecretSanitizer()

	tests := []struct {
		name     string
		envVars  []string
		expected []string
	}{
		{
			name: "OPENSEARCH_INITIAL_ADMIN_PASSWORD should be redacted",
			envVars: []string{
				"OPENSEARCH_INITIAL_ADMIN_PASSWORD=mysecretpassword",
				"NORMAL_VAR=normalvalue",
			},
			expected: []string{
				"OPENSEARCH_INITIAL_ADMIN_PASSWORD=***REDACTED***",
				"NORMAL_VAR=normalvalue",
			},
		},
		{
			name: "Various password patterns",
			envVars: []string{
				"PASSWORD=secret",
				"DB_PASSWORD=secret",
				"ADMIN_PASSWORD_HASH=secret",
				"MY_SECRET_KEY=secret",
				"API_TOKEN=secret",
				"ENCRYPTION_KEY=secret",
			},
			expected: []string{
				"PASSWORD=***REDACTED***",
				"DB_PASSWORD=***REDACTED***",
				"ADMIN_PASSWORD_HASH=***REDACTED***",
				"MY_SECRET_KEY=***REDACTED***",
				"API_TOKEN=***REDACTED***",
				"ENCRYPTION_KEY=***REDACTED***",
			},
		},
		{
			name: "Non-sensitive variables should not be redacted",
			envVars: []string{
				"DATABASE_HOST=localhost",
				"PORT=8080",
				"LOG_LEVEL=debug",
			},
			expected: []string{
				"DATABASE_HOST=localhost",
				"PORT=8080",
				"LOG_LEVEL=debug",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyEnvVarSanitization(t, sanitizer, tt.envVars, tt.expected)
		})
	}
}

// verifyEnvVarSanitization verifies that environment variables are sanitized correctly.
func verifyEnvVarSanitization(t *testing.T, sanitizer *SecretSanitizer, envVars, expected []string) {
	t.Helper()
	result := sanitizer.SanitizeEnvVars(envVars)

	if len(result) != len(expected) {
		t.Errorf("Expected %d env vars, got %d", len(expected), len(result))

		return
	}

	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("Expected env var %d to be %q, got %q", i, exp, result[i])
		}
	}
}

func TestSecretSanitizer_IsSensitiveKey(t *testing.T) {
	sanitizer := NewSecretSanitizer()

	tests := []struct {
		key      string
		expected bool
	}{
		{"OPENSEARCH_INITIAL_ADMIN_PASSWORD", true},
		{"PASSWORD", true},
		{"DB_PASSWORD", true},
		{"ADMIN_PASSWORD_HASH", true},
		{"SECRET_KEY", true},
		{"API_TOKEN", true},
		{"ENCRYPTION_KEY", true},
		{"DATABASE_HOST", false},
		{"PORT", false},
		{"LOG_LEVEL", false},
		{"HOSTNAME", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := sanitizer.isSensitiveKey(tt.key)
			if result != tt.expected {
				t.Errorf("isSensitiveKey(%q) = %v, expected %v", tt.key, result, tt.expected)
			}
		})
	}
}

func TestSecretSanitizer_SanitizeYAML(t *testing.T) {
	sanitizer := NewSecretSanitizer()

	input := `
apiVersion: v1
kind: Secret
metadata:
  name: opensearch-credentials
data:
  password: bXlzZWNyZXRwYXNzd29yZA==
  OPENSEARCH_INITIAL_ADMIN_PASSWORD: c2VjcmV0MTIz
  username: admin
`

	result := sanitizer.SanitizeYAML(input)

	// Check that password fields are redacted
	if !strings.Contains(result, "password: ***REDACTED***") {
		t.Error("Expected 'password' to be redacted")
	}
	if !strings.Contains(result, "OPENSEARCH_INITIAL_ADMIN_PASSWORD: ***REDACTED***") {
		t.Error("Expected 'OPENSEARCH_INITIAL_ADMIN_PASSWORD' to be redacted")
	}
	// Check that non-sensitive fields are not redacted
	if !strings.Contains(result, "username: admin") {
		t.Error("Expected 'username' to not be redacted")
	}
}

func TestSecretSanitizer_SanitizeJSON_WithEnvArray(t *testing.T) {
	sanitizer := NewSecretSanitizer()

	// Simulate container inspect JSON with Env array
	input := `{
  "Config": {
    "Env": [
      "OPENSEARCH_INITIAL_ADMIN_PASSWORD=AiServices@12345",
      "OPENSEARCH_JAVA_OPTS=-Xms4g -Xmx4g",
      "PATH=/usr/local/sbin:/usr/local/bin",
      "JAVA_HOME=/usr/share/opensearch/jdk",
      "HOME=/usr/share/opensearch"
    ]
  }
}`

	result, err := sanitizer.SanitizeJSON([]byte(input))
	if err != nil {
		t.Fatalf("SanitizeJSON failed: %v", err)
	}

	resultStr := string(result)

	// Check that OPENSEARCH_INITIAL_ADMIN_PASSWORD is redacted
	if !strings.Contains(resultStr, "OPENSEARCH_INITIAL_ADMIN_PASSWORD=***REDACTED***") {
		t.Error("Expected 'OPENSEARCH_INITIAL_ADMIN_PASSWORD' to be redacted in Env array")
	}

	// Check that non-sensitive env vars are not redacted
	if !strings.Contains(resultStr, "JAVA_HOME=/usr/share/opensearch/jdk") {
		t.Error("Expected 'JAVA_HOME' to not be redacted")
	}
	if !strings.Contains(resultStr, "HOME=/usr/share/opensearch") {
		t.Error("Expected 'HOME' to not be redacted")
	}
}

// Made with Bob
