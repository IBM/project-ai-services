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
	minAnnotationParts   = 2
	keyValueParts        = 2
	maxPasswordTypeParts = 2 // max parts when splitting "password options"
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
// At least one character from each enabled category is guaranteed to be present.
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
// Guarantees at least one character from each enabled category.
func generateRandomPasswordWithOptions(opts passwordOptions) (string, error) {
	if err := validatePasswordOptions(opts); err != nil {
		return "", err
	}

	requiredSets := buildRequiredCharsets(opts)
	fullCharset := buildPasswordCharset(opts)

	password, err := generatePasswordBytes(opts.Length, requiredSets, fullCharset)
	if err != nil {
		return "", err
	}

	ensureFirstCharNotSpecial(password, opts)

	return string(password), nil
}

// validatePasswordOptions validates the password generation options.
func validatePasswordOptions(opts passwordOptions) error {
	if opts.Length <= 0 {
		return fmt.Errorf("password length must be greater than 0")
	}

	requiredSets := buildRequiredCharsets(opts)
	if len(requiredSets) == 0 {
		return fmt.Errorf("at least one character type must be enabled")
	}

	if opts.Length < len(requiredSets) {
		return fmt.Errorf("password length (%d) must be at least %d to satisfy all character requirements", opts.Length, len(requiredSets))
	}

	return nil
}

// generatePasswordBytes generates password bytes with guaranteed character requirements.
func generatePasswordBytes(length int, requiredSets []string, fullCharset string) ([]byte, error) {
	password := make([]byte, length)

	// Place one character from each required set
	for i, charset := range requiredSets {
		char, err := randomCharFromSet(charset)
		if err != nil {
			return nil, err
		}
		password[i] = char
	}

	// Fill remaining positions with random characters
	for i := len(requiredSets); i < length; i++ {
		char, err := randomCharFromSet(fullCharset)
		if err != nil {
			return nil, err
		}
		password[i] = char
	}

	// Shuffle to avoid predictable patterns
	if err := shuffleBytes(password); err != nil {
		return nil, err
	}

	return password, nil
}

// ensureFirstCharNotSpecial ensures the first character is not special when non-special types exist.
func ensureFirstCharNotSpecial(password []byte, opts passwordOptions) {
	hasNonSpecial := opts.Lower || opts.Upper || opts.Digits
	if !opts.Special || !hasNonSpecial || !isSpecialChar(password[0]) {
		return
	}

	// Find first non-special character and swap
	for i := 1; i < len(password); i++ {
		if !isSpecialChar(password[i]) {
			password[0], password[i] = password[i], password[0]

			break
		}
	}
}

// buildRequiredCharsets returns a slice of character sets that must have at least one character.
func buildRequiredCharsets(opts passwordOptions) []string {
	var sets []string

	if opts.Lower {
		sets = append(sets, lowercaseChars)
	}
	if opts.Upper {
		sets = append(sets, uppercaseChars)
	}
	if opts.Digits {
		sets = append(sets, digitChars)
	}
	if opts.Special {
		sets = append(sets, specialChars)
	}

	return sets
}

// buildPasswordCharset builds the complete character set based on options.
func buildPasswordCharset(opts passwordOptions) string {
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
	if opts.Special {
		charset.WriteString(specialChars)
	}

	return charset.String()
}

// randomCharFromSet returns a random character from the given character set.
func randomCharFromSet(charset string) (byte, error) {
	if len(charset) == 0 {
		return 0, fmt.Errorf("charset cannot be empty")
	}

	charsetLen := big.NewInt(int64(len(charset)))
	randomIndex, err := rand.Int(rand.Reader, charsetLen)
	if err != nil {
		return 0, fmt.Errorf("failed to generate random character: %w", err)
	}

	return charset[randomIndex.Int64()], nil
}

// shuffleBytes performs a Fisher-Yates shuffle on the byte slice using crypto/rand.
func shuffleBytes(data []byte) error {
	n := len(data)
	for i := n - 1; i > 0; i-- {
		// Generate random index from 0 to i (inclusive)
		maxIdx := big.NewInt(int64(i + 1))
		randomIdx, err := rand.Int(rand.Reader, maxIdx)
		if err != nil {
			return fmt.Errorf("failed to shuffle password: %w", err)
		}

		j := randomIdx.Int64()
		data[i], data[j] = data[j], data[i]
	}

	return nil
}

// isSpecialChar checks if a byte is a special character.
func isSpecialChar(b byte) bool {
	return strings.ContainsRune(specialChars, rune(b))
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
func hasGenerateAnnotation(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	return strings.Contains(n.HeadComment, "@generate:")
}

// extractGenerateAnnotation extracts the @generate annotation from a yaml.Node's HeadComment.
// Returns the full annotation string (e.g., "@generate:password" or "@generate:password length=24").
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

	// Remove @generate: prefix and split the rest
	if !strings.HasPrefix(annotation, "@generate:") {
		return opts, fmt.Errorf("invalid annotation format: %s", annotation)
	}

	// Get everything after @generate:
	rest := strings.TrimPrefix(annotation, "@generate:")
	rest = strings.TrimSpace(rest)

	// Split by space to separate "password" from options
	parts := strings.SplitN(rest, " ", maxPasswordTypeParts)
	if len(parts) == 0 || parts[0] != "password" {
		return opts, fmt.Errorf("invalid annotation format: %s", annotation)
	}

	// If there are options, parse them
	if len(parts) == maxPasswordTypeParts {
		if err := parseOptions(parts[1], &opts); err != nil {
			return opts, err
		}
	}

	return opts, nil
}

// parseOptions parses key=value pairs and updates password options.
func parseOptions(optStr string, opts *passwordOptions) error {
	pairs := strings.SplitSeq(optStr, ",")
	for pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		kv := strings.SplitN(pair, "=", keyValueParts)
		if len(kv) != keyValueParts {
			return fmt.Errorf("invalid option format: %s (expected key=value)", pair)
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		if err := applyOption(key, value, opts); err != nil {
			return err
		}
	}

	return nil
}

// applyOption applies a single option to password options.
func applyOption(key, value string, opts *passwordOptions) error {
	switch key {
	case "length":
		length, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid length value: %s", value)
		}
		if length <= 0 {
			return fmt.Errorf("length must be greater than 0")
		}
		opts.Length = length

	case "lower":
		opts.Lower = value == "true"

	case "upper":
		opts.Upper = value == "true"

	case "digits":
		opts.Digits = value == "true"

	case "special":
		opts.Special = value == "true"

	default:
		return fmt.Errorf("unknown option: %s", key)
	}

	return nil
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
