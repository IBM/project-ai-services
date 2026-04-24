package templates

import (
	"os"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	// EnvAppPath is the environment variable for custom application path
	EnvAppPath = "AI_SERVICES_APP_PATH"
)

// GetTemplateProvider creates and returns the appropriate template provider.
// It checks for custom application path from customPath parameter, global variable, or environment variable,
// and creates a composite provider if custom path is specified.
// customPath: Custom application path (empty string to check global variable and environment variable)
func GetTemplateProvider(customPath string) Template {
	// Determine the custom app path: parameter > global var > env var
	appPath := customPath
	if appPath == "" {
		// Import vars package to access global CustomAppPath
		appPath = vars.CustomAppPath
	}
	if appPath == "" {
		appPath = os.Getenv(EnvAppPath)
	}

	// If no custom path specified, use embedded templates only
	if appPath == "" {
		return NewEmbedTemplateProvider(&assets.ApplicationFS)
	}

	// Custom path specified - create composite provider
	logger.Infof("Using custom application path: %s\n", appPath)

	// Create filesystem provider for custom applications
	fsProvider, err := NewFilesystemTemplateProvider(appPath)
	if err != nil {
		logger.Warningf("Failed to load custom applications from %s: %v\n", appPath, err)
		logger.Warningf("Falling back to embedded applications only\n")
		return NewEmbedTemplateProvider(&assets.ApplicationFS)
	}

	// Create embedded provider for built-in applications
	embedProvider := NewEmbedTemplateProvider(&assets.ApplicationFS)

	// Create composite provider with custom taking precedence (listed first)
	return NewCompositeTemplateProvider(fsProvider, embedProvider)
}

// Made with Bob
