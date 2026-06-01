package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// DefaultPasswordLength is the default length for generated passwords.
	DefaultPasswordLength = 16
	// passwordCharset contains all characters used for password generation.
	passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
)

// GenerateRandomPassword generates a cryptographically secure random password
// of the specified length using crypto/rand.
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("password length must be greater than 0")
	}

	password := make([]byte, length)
	charsetLen := big.NewInt(int64(len(passwordCharset)))

	for i := 0; i < length; i++ {
		randomIndex, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random password: %w", err)
		}
		password[i] = passwordCharset[randomIndex.Int64()]
	}

	return string(password), nil
}

// ProcessGenerateAnnotationsFromYAML processes @generate annotations in raw YAML data.
// It parses the YAML with comments preserved, checks for @generate annotations in HeadComments,
// and replaces empty string values with generated values.
// Returns the processed YAML as bytes.
// Supported annotations:
//   - @generate:password - generates a random password using GenerateRandomPassword
//   - @generate:password:length - generates a password of specified length (e.g., @generate:password:20)
func ProcessGenerateAnnotationsFromYAML(yamlData []byte) ([]byte, error) {
	// Parse into yaml.Node to preserve comments
	var rootNode yaml.Node
	if err := yaml.Unmarshal(yamlData, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML with comments: %w", err)
	}

	// Process the node tree
	if err := processNodeForGenerate(&rootNode); err != nil {
		return nil, err
	}

	// Marshal back to YAML bytes
	processedData, err := yaml.Marshal(&rootNode)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal processed YAML: %w", err)
	}

	return processedData, nil
}

// processNodeForGenerate recursively processes yaml.Node tree looking for @generate annotations.
func processNodeForGenerate(node *yaml.Node) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.DocumentNode:
		// Process document content
		for _, child := range node.Content {
			if err := processNodeForGenerate(child); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		// Process key-value pairs
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			// Check if the value node has a @generate annotation in its HeadComment
			if hasGenerateAnnotation(valueNode) {
				// Generate the value based on the annotation
				annotation := extractGenerateAnnotation(valueNode)
				generated, err := generateValue(annotation)
				if err != nil {
					return fmt.Errorf("failed to generate value for key '%s': %w", keyNode.Value, err)
				}
				// Replace the value
				valueNode.Value = generated
			}

			// Recursively process nested structures
			if err := processNodeForGenerate(valueNode); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		// Process array elements
		for _, child := range node.Content {
			if err := processNodeForGenerate(child); err != nil {
				return err
			}
		}
	}

	return nil
}

// hasGenerateAnnotation checks if a yaml.Node has a @generate annotation in its HeadComment.
func hasGenerateAnnotation(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	return strings.Contains(n.HeadComment, "@generate:")
}

// extractGenerateAnnotation extracts the @generate annotation from a yaml.Node's HeadComment.
func extractGenerateAnnotation(n *yaml.Node) string {
	if n == nil {
		return ""
	}

	comment := n.HeadComment
	idx := strings.Index(comment, "@generate:")
	if idx < 0 {
		return ""
	}

	// Extract the annotation (e.g., "@generate:password" or "@generate:password:16")
	annotation := comment[idx:]
	// Take only the first line if there are multiple lines
	if newlineIdx := strings.Index(annotation, "\n"); newlineIdx > 0 {
		annotation = annotation[:newlineIdx]
	}

	return strings.TrimSpace(annotation)
}

// generateValue generates a value based on the annotation string.
func generateValue(annotation string) (string, error) {
	parts := strings.Split(annotation, ":")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid annotation format: %s", annotation)
	}

	annotationType := parts[1]
	switch annotationType {
	case "password":
		// Check if a custom length is specified
		length := DefaultPasswordLength
		if len(parts) > 2 {
			var err error
			_, err = fmt.Sscanf(parts[2], "%d", &length)
			if err != nil {
				return "", fmt.Errorf("invalid password length in annotation '%s': %w", annotation, err)
			}
		}
		return GenerateRandomPassword(length)
	default:
		return "", fmt.Errorf("unsupported annotation type: %s", annotationType)
	}
}

// Made with Bob
