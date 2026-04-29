package mustgather

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	redactedValue = "***REDACTED***"
)

// SecretSanitizer handles sanitization of sensitive data.
type SecretSanitizer struct {
	// Patterns to match sensitive keys
	sensitiveKeyPatterns []*regexp.Regexp
}

// NewSecretSanitizer creates a new SecretSanitizer.
func NewSecretSanitizer() *SecretSanitizer {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i).*password.*`),
		regexp.MustCompile(`(?i).*secret.*`),
		regexp.MustCompile(`(?i).*token.*`),
		regexp.MustCompile(`(?i).*key.*`),
		regexp.MustCompile(`(?i).*credential.*`),
		regexp.MustCompile(`(?i).*auth.*`),
		regexp.MustCompile(`(?i).*private.*`),
		regexp.MustCompile(`(?i).*cert.*`),
	}

	return &SecretSanitizer{
		sensitiveKeyPatterns: patterns,
	}
}

// isSensitiveKey checks if a key name indicates sensitive data.
func (s *SecretSanitizer) isSensitiveKey(key string) bool {
	for _, pattern := range s.sensitiveKeyPatterns {
		if pattern.MatchString(key) {
			return true
		}
	}

	return false
}

// SanitizeJSON sanitizes sensitive data in JSON content.
func (s *SecretSanitizer) SanitizeJSON(data []byte) ([]byte, error) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		// If not valid JSON, return as is
		return data, nil
	}

	sanitized := s.sanitizeValue(obj)

	return json.MarshalIndent(sanitized, "", "  ")
}

// sanitizeValue recursively sanitizes values in maps and slices.
func (s *SecretSanitizer) sanitizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			if s.isSensitiveKey(key) {
				result[key] = redactedValue
			} else {
				result[key] = s.sanitizeValue(val)
			}
		}

		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = s.sanitizeValue(val)
		}

		return result
	case string:
		// Check if string looks like an environment variable (KEY=VALUE)
		if strings.Contains(v, "=") {
			return s.sanitizeEnvString(v)
		}

		return v
	default:
		return v
	}
}

// sanitizeEnvString sanitizes a string that looks like an environment variable.
func (s *SecretSanitizer) sanitizeEnvString(envStr string) string {
	const keyValueParts = 2
	parts := strings.SplitN(envStr, "=", keyValueParts)
	if len(parts) == keyValueParts {
		key := parts[0]
		if s.isSensitiveKey(key) {
			return key + "=" + redactedValue
		}
	}

	return envStr
}

// SanitizeYAML sanitizes sensitive data in YAML-like text content.
func (s *SecretSanitizer) SanitizeYAML(content string) string {
	lines := strings.Split(content, "\n")
	sanitized := make([]string, 0, len(lines))

	const keyValueParts = 2
	for _, line := range lines {
		// Check if line contains a key-value pair
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", keyValueParts)
			if len(parts) == keyValueParts {
				key := strings.TrimSpace(parts[0])
				if s.isSensitiveKey(key) {
					// Preserve indentation
					indent := len(line) - len(strings.TrimLeft(line, " \t"))
					sanitized = append(sanitized, strings.Repeat(" ", indent)+key+": "+redactedValue)

					continue
				}
			}
		}
		sanitized = append(sanitized, line)
	}

	return strings.Join(sanitized, "\n")
}

// SanitizeEnvVars sanitizes environment variables.
func (s *SecretSanitizer) SanitizeEnvVars(envVars []string) []string {
	sanitized := make([]string, 0, len(envVars))

	const keyValueParts = 2
	for _, env := range envVars {
		parts := strings.SplitN(env, "=", keyValueParts)
		if len(parts) == keyValueParts {
			key := parts[0]
			if s.isSensitiveKey(key) {
				sanitized = append(sanitized, key+"="+redactedValue)

				continue
			}
		}
		sanitized = append(sanitized, env)
	}

	return sanitized
}

// SanitizeText sanitizes sensitive data in plain text.
func (s *SecretSanitizer) SanitizeText(content string) string {
	// Try JSON first
	if jsonData, err := s.SanitizeJSON([]byte(content)); err == nil {
		if json.Valid(jsonData) {
			return string(jsonData)
		}
	}

	// Fall back to YAML-style sanitization
	return s.SanitizeYAML(content)
}

// Made with Bob
