package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// DefaultPasswordLength is the default length for generated passwords.
	DefaultPasswordLength = 16
	// Character sets for password generation.
	lowercaseChars = "abcdefghijklmnopqrstuvwxyz"
	uppercaseChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digitChars     = "0123456789"
	specialChars   = "@#$%^*-_+"

	// Annotation parsing constants.
	minAnnotationParts     = 2
	annotationPartsWithOpt = 3
	keyValueParts          = 2
)

// passwordOptions contains options for password generation.
type passwordOptions struct {
	Length  int
	Lower   bool
	Upper   bool
	Digits  bool
	Special bool
}

// GenerateRandomPassword generates a cryptographically secure random password with default settings.
// The password will be 16 characters long and include uppercase, lowercase, digits, and special characters.
// The first character will always be alphanumeric (not a special character).
// TODO: This is currently being used in Catalog for DB password which should be later moved to use new @generate annotation.
func GenerateRandomPassword() (string, error) {
	return generateRandomPasswordWithOptions(passwordOptions{
		Length:  DefaultPasswordLength,
		Lower:   true,
		Upper:   true,
		Digits:  true,
		Special: true,
	})
}

// generateRandomPasswordWithOptions generates a cryptographically secure random password
// with the specified options using crypto/rand.
func generateRandomPasswordWithOptions(opts passwordOptions) (string, error) {
	if opts.Length <= 0 {
		return "", fmt.Errorf("password length must be greater than 0")
	}

	charset := buildPasswordCharset(opts, true)
	if charset == "" {
		return "", fmt.Errorf("at least one character type must be enabled")
	}

	firstCharset := buildPasswordCharset(opts, false)
	if firstCharset == "" {
		firstCharset = charset
	}

	password := make([]byte, opts.Length)

	firstChar, err := randomCharsetByte(firstCharset)
	if err != nil {
		return "", err
	}
	password[0] = firstChar

	for i := 1; i < opts.Length; i++ {
		char, err := randomCharsetByte(charset)
		if err != nil {
			return "", err
		}
		password[i] = char
	}

	return string(password), nil
}

func buildPasswordCharset(opts passwordOptions, includeSpecial bool) string {
	var charset strings.Builder

	if opts.Lower {
		charset.WriteString(lowercaseChars)
	}
	if opts.Upper {
		charset.WriteString(uppercaseChars)
	}
	if opts.Digits {
		charset.WriteString(digitChars)
	}
	if includeSpecial && opts.Special {
		charset.WriteString(specialChars)
	}

	return charset.String()
}

func randomCharsetByte(charset string) (byte, error) {
	charsetLen := big.NewInt(int64(len(charset)))
	randomIndex, err := rand.Int(rand.Reader, charsetLen)
	if err != nil {
		return 0, fmt.Errorf("failed to generate random password: %w", err)
	}

	return charset[randomIndex.Int64()], nil
}

// ProcessGenerateAnnotationsFromYAML processes @generate annotations in raw YAML data.
// It parses the YAML with comments preserved, checks for @generate annotations in HeadComments,
// and replaces empty string values with generated values.
// Returns the processed YAML as bytes.
// Supported annotations:
//   - @generate:password - generates a random password with default options
//   - @generate:password length=24, special=true, upper=true - generates with custom options
func ProcessGenerateAnnotationsFromYAML(yamlData []byte) ([]byte, error) {
	// Parse into yaml.Node to preserve comments
	var rootNode yaml.Node
	if err := yaml.Unmarshal(yamlData, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML with comments: %w", err)
	}

	// Create a processor function for @generate annotations
	generateProcessor := func(keyNode, valueNode *yaml.Node) error {
		// Check if the KEY node has a @generate annotation
		if keyNode != nil && hasGenerateAnnotation(keyNode) {
			annotation := extractGenerateAnnotation(keyNode)
			generated, err := generateValue(annotation)
			if err != nil {
				return fmt.Errorf("failed to generate value for key '%s': %w", keyNode.Value, err)
			}
			valueNode.Value = generated
		}

		return nil
	}

	// Use the generic ProcessYAMLNode function from util.go
	if err := ProcessYAMLNode(&rootNode, generateProcessor); err != nil {
		return nil, err
	}

	// Marshal back to YAML bytes
	processedData, err := yaml.Marshal(&rootNode)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal processed YAML: %w", err)
	}

	return processedData, nil
}

// hasGenerateAnnotation checks if a yaml.Node has a @generate annotation in its HeadComment.
// Similar to isHidden in util.go.
func hasGenerateAnnotation(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	return strings.Contains(n.HeadComment, "@generate:")
}

// extractGenerateAnnotation extracts the @generate annotation from a yaml.Node's HeadComment.
// Returns the full annotation string (e.g., "@generate:password" or "@generate:password length=24").
// Similar to getDescription in util.go.
func extractGenerateAnnotation(n *yaml.Node) string {
	if n == nil {
		return ""
	}

	comment := n.HeadComment
	idx := strings.Index(comment, "@generate:")
	if idx < 0 {
		return ""
	}

	// Extract the annotation starting from @generate:
	annotation := comment[idx:]
	// Take only the first line if there are multiple lines
	if newlineIdx := strings.Index(annotation, "\n"); newlineIdx > 0 {
		annotation = annotation[:newlineIdx]
	}

	return strings.TrimSpace(annotation)
}

// parsePasswordOptions parses password options from annotation string.
// Format: @generate:password length=24, special=true, upper=true.
func parsePasswordOptions(annotation string) (passwordOptions, error) {
	// Default options
	opts := passwordOptions{
		Length:  DefaultPasswordLength,
		Lower:   true,
		Upper:   true,
		Digits:  true,
		Special: true,
	}

	// Remove @generate:password prefix
	parts := strings.SplitN(annotation, ":", annotationPartsWithOpt)
	if len(parts) < minAnnotationParts || parts[1] != "password" {
		return opts, fmt.Errorf("invalid annotation format: %s", annotation)
	}

	// If there's a third part, parse the options
	if len(parts) == annotationPartsWithOpt {
		parseOptions(parts[2], &opts)
	}

	return opts, nil
}

// parseOptions parses key=value pairs and updates password options.
func parseOptions(optStr string, opts *passwordOptions) {
	pairs := strings.SplitSeq(optStr, ",")
	for pair := range pairs {
		pair = strings.TrimSpace(pair)
		kv := strings.SplitN(pair, "=", keyValueParts)
		if len(kv) != keyValueParts {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		applyOption(key, value, opts)
	}
}

// applyOption applies a single option to password options.
func applyOption(key, value string, opts *passwordOptions) {
	switch key {
	case "length":
		if length, err := strconv.Atoi(value); err == nil {
			opts.Length = length
		}
	case "lower":
		opts.Lower = value == "true"
	case "upper":
		opts.Upper = value == "true"
	case "digits":
		opts.Digits = value == "true"
	case "special":
		opts.Special = value == "true"
	}
}

// generateValue generates a value based on the annotation string.
func generateValue(annotation string) (string, error) {
	parts := strings.Split(annotation, ":")
	if len(parts) < minAnnotationParts {
		return "", fmt.Errorf("invalid annotation format: %s", annotation)
	}

	annotationType := parts[1]
	switch annotationType {
	case "password":
		opts, err := parsePasswordOptions(annotation)
		if err != nil {
			return "", err
		}

		return generateRandomPasswordWithOptions(opts)
	default:
		return "", fmt.Errorf("unsupported annotation type: %s", annotationType)
	}
}

// Made with Bob
