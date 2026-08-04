package common

import "fmt"

const (
	// MaxIndexNameLength is the maximum allowed length for an OpenSearch index name.
	MaxIndexNameLength = 255
)

// ValidateIndexName validates an OpenSearch index name to prevent command injection.
// OpenSearch index names must be lowercase and can contain: letters, numbers, -, _, +, .
func ValidateIndexName(indexName string) error {
	if len(indexName) == 0 || len(indexName) > MaxIndexNameLength {
		return fmt.Errorf("invalid index name length: %d", len(indexName))
	}

	// Reject names starting with special characters
	if indexName[0] == '-' || indexName[0] == '_' || indexName[0] == '+' {
		return fmt.Errorf("index name cannot start with special character")
	}

	// Validate all characters
	for _, char := range indexName {
		if !IsValidIndexChar(char) {
			return fmt.Errorf("invalid character in index name: %c", char)
		}
	}

	return nil
}

// IsValidIndexChar checks if a character is valid for an OpenSearch index name.
func IsValidIndexChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9') ||
		char == '-' || char == '_' || char == '+' || char == '.'
}

// Made with Bob
