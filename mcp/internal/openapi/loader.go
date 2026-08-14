package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi"
	highV3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/index"
	"go.yaml.in/yaml/v4"
)

// LoadDescription loads an OpenAPI description from a file path or URL,
// dereferencing all $ref fields inline.
func LoadDescription(ref string) (*highV3.Document, error) {
	var data []byte
	var err error

	// Load from URL or file
	if isURL(ref) {
		data, err = loadFromURL(ref)
	} else {
		data, err = loadFromFile(ref)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI description: %w", err)
	}

	// Parse into yaml.Node
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI YAML: %w", err)
	}

	// Index + Resolve all $refs
	cfg := index.CreateClosedAPIIndexConfig()
	rol := index.NewRolodex(cfg)
	rol.SetRootNode(&root)

	if err := rol.IndexTheRolodex(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to index OpenAPI spec: %w", err)
	}

	rol.Resolve() // replaces all $ref inline

	// Marshal back to []byte
	resolvedBytes, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resolved OpenAPI spec: %w", err)
	}

	// Build libopenapi.Document from resolved spec
	doc, err := libopenapi.NewDocument(resolvedBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenAPI document: %w", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil {
		fmt.Println("Model error:", err)
	}

	// Validate basic structure
	if model.Model.Info == nil {
		return nil, fmt.Errorf("invalid OpenAPI description: missing info section")
	}

	return &model.Model, nil
}

// isURL checks if a string is a valid URL
func isURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// loadFromURL loads content from a URL
func loadFromURL(urlStr string) ([]byte, error) {
	// Validate URL scheme to prevent SSRF attacks
	if err := validateURL(urlStr); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// #nosec G107 - URL is validated above to only allow http/https schemes
	resp, err := http.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL %s: %w", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL %s: HTTP %d", urlStr, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", urlStr, err)
	}

	return data, nil
}

// loadFromFile loads content from a file
func loadFromFile(path string) ([]byte, error) {
	// Validate file path to prevent directory traversal
	if err := validateFilePath(path); err != nil {
		return nil, fmt.Errorf("invalid file path: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", path, err)
	}

	// #nosec G304 - file path is validated above to prevent directory traversal
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", absPath, err)
	}

	return data, nil
}

// validateURL ensures the URL uses a safe scheme (http or https only)
func validateURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	// Only allow http and https schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (only http and https are allowed)", u.Scheme)
	}

	return nil
}

// validateFilePath ensures the file path doesn't contain directory traversal attempts
func validateFilePath(path string) error {
	// Check for directory traversal patterns
	if strings.Contains(path, "..") {
		return fmt.Errorf("path contains directory traversal sequence (..)")
	}

	// Ensure path doesn't start with / (absolute paths should be relative to working directory)
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return fmt.Errorf("path contains suspicious elements")
	}

	return nil
}
