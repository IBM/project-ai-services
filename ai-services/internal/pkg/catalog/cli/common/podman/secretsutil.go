package podman

import (
	"context"
	"fmt"

	"github.com/containers/podman/v5/pkg/bindings/secrets"
)

// ReadSecretFromPodman retrieves the value for a known key from a live Podman secret.
// The secret data format stored by catalog configure is "key: value\n".
// The returned value is never logged.
func ReadSecretFromPodman(ctx context.Context, secretName, key string) (string, error) {
	opts := &secrets.InspectOptions{}
	opts.WithShowSecret(true)

	info, err := secrets.Inspect(ctx, secretName, opts)
	if err != nil {
		return "", fmt.Errorf("failed to inspect secret %s: %w", secretName, err)
	}

	if info.SecretData == "" {
		return "", fmt.Errorf("secret %s is empty", secretName)
	}

	value, err := ExtractKeyFromSecretData(info.SecretData, key)
	if err != nil {
		return "", fmt.Errorf("secret %s: %w", secretName, err)
	}

	return value, nil
}

// ExtractKeyFromSecretData parses "key: value\n" lines and returns the value for
// the requested key.  The raw secret data is never surfaced in error messages.
func ExtractKeyFromSecretData(secretData, wantKey string) (string, error) {
	for _, line := range splitLines(secretData) {
		k, v, ok := splitKeyValue(line)
		if ok && k == wantKey {
			return v, nil
		}
	}

	return "", fmt.Errorf("key %q not found in secret data", wantKey)
}

// splitLines splits a string into trimmed, non-empty lines.
func splitLines(s string) []string {
	var result []string

	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := trimSpaces(s[start:i])
			if line != "" {
				result = append(result, line)
			}
			start = i + 1
		}
	}

	return result
}

// splitKeyValue splits "key: value" into its parts (trimmed).
func splitKeyValue(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			return trimSpaces(line[:i]), trimSpaces(line[i+1:]), true
		}
	}

	return "", "", false
}

// trimSpaces trims leading and trailing ASCII whitespace without importing strings.
func trimSpaces(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}

	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

// Made with Bob
