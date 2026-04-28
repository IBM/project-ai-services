package templates

import (
	"fmt"
	"text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"

	"helm.sh/helm/v4/pkg/chart"
)

// compositeTemplateProvider combines multiple template providers with priority ordering.
// Custom providers take precedence over embedded providers for applications with the same name.
type compositeTemplateProvider struct {
	providers []Template
}

// NewCompositeTemplateProvider creates a new composite template provider.
// Providers are checked in order, with earlier providers taking precedence.
func NewCompositeTemplateProvider(providers ...Template) Template {
	return &compositeTemplateProvider{
		providers: providers,
	}
}

// ListApplications lists all available application templates from all providers.
// If an application exists in multiple providers, it's only listed once.
func (c *compositeTemplateProvider) ListApplications(hidden bool) ([]string, error) {
	appSet := make(map[string]bool)
	var apps []string

	for i, provider := range c.providers {
		providerApps, err := provider.ListApplications(hidden)
		if err != nil {
			logger.Warningf("Failed to list applications from provider %d: %v\n", i, err)
			continue
		}

		for _, app := range providerApps {
			if !appSet[app] {
				appSet[app] = true
				apps = append(apps, app)
			}
		}
	}

	return apps, nil
}

// AppTemplateExist checks if the application exists in any provider.
func (c *compositeTemplateProvider) AppTemplateExist(app string) error {
	var lastErr error

	for _, provider := range c.providers {
		err := provider.AppTemplateExist(app)
		if err == nil {
			return nil // Found in this provider
		}
		lastErr = err
	}

	// If not found in any provider, return the last error
	if lastErr != nil {
		return lastErr
	}

	return fmt.Errorf("application template '%s' does not exist", app)
}

// ListApplicationTemplateValues lists template values from the first provider that has the app.
func (c *compositeTemplateProvider) ListApplicationTemplateValues(app string) (map[string]string, error) {
	for i, provider := range c.providers {
		values, err := provider.ListApplicationTemplateValues(app)
		if err == nil {
			if i > 0 {
				logger.Infof("Using application '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return values, nil
		}
		// If error is not "runtime not supported", continue to next provider
		if err != ErrRuntimeNotSupported {
			logger.Infof("Provider %d failed to list values for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
		}
	}

	return nil, fmt.Errorf("application template '%s' not found in any provider", app)
}

// LoadAllTemplates loads templates from the first provider that has the app.
func (c *compositeTemplateProvider) LoadAllTemplates(app string) (map[string]*template.Template, error) {
	for i, provider := range c.providers {
		templates, err := provider.LoadAllTemplates(app)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading templates for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return templates, nil
		}
		logger.Infof("Provider %d failed to load templates for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load templates for application '%s' from any provider", app)
}

// LoadPodTemplate loads a pod template from the first provider that has the app.
func (c *compositeTemplateProvider) LoadPodTemplate(app, file string, params any) (*models.PodSpec, error) {
	for i, provider := range c.providers {
		spec, err := provider.LoadPodTemplate(app, file, params)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading pod template '%s' for '%s' from custom provider\n", file, app, logger.VerbosityLevelDebug)
			}
			return spec, nil
		}
		logger.Infof("Provider %d failed to load pod template '%s' for '%s': %v\n", i, file, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load pod template '%s' for application '%s' from any provider", file, app)
}

// LoadPodTemplateWithValues loads a pod template with values from the first provider that has the app.
func (c *compositeTemplateProvider) LoadPodTemplateWithValues(app, file, appName string, valuesFileOverrides []string, cliOverrides map[string]string) (*models.PodSpec, error) {
	for i, provider := range c.providers {
		spec, err := provider.LoadPodTemplateWithValues(app, file, appName, valuesFileOverrides, cliOverrides)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading pod template with values '%s' for '%s' from custom provider\n", file, app, logger.VerbosityLevelDebug)
			}
			return spec, nil
		}
		logger.Infof("Provider %d failed to load pod template with values '%s' for '%s': %v\n", i, file, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load pod template with values '%s' for application '%s' from any provider", file, app)
}

// LoadValues loads values from the first provider that has the app.
func (c *compositeTemplateProvider) LoadValues(app string, valuesFileOverrides []string, cliOverrides map[string]string) (map[string]interface{}, error) {
	for i, provider := range c.providers {
		values, err := provider.LoadValues(app, valuesFileOverrides, cliOverrides)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading values for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return values, nil
		}
		logger.Infof("Provider %d failed to load values for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load values for application '%s' from any provider", app)
}

// LoadMetadata loads metadata from the first provider that has the app.
func (c *compositeTemplateProvider) LoadMetadata(app string, isRuntime bool) (*AppMetadata, error) {
	for i, provider := range c.providers {
		metadata, err := provider.LoadMetadata(app, isRuntime)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading metadata for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return metadata, nil
		}
		logger.Infof("Provider %d failed to load metadata for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load metadata for application '%s' from any provider", app)
}

// LoadMdFiles loads markdown files from the first provider that has the app.
func (c *compositeTemplateProvider) LoadMdFiles(app string) (map[string]*template.Template, error) {
	for i, provider := range c.providers {
		mdFiles, err := provider.LoadMdFiles(app)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading markdown files for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return mdFiles, nil
		}
		logger.Infof("Provider %d failed to load markdown files for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load markdown files for application '%s' from any provider", app)
}

// LoadVarsFile loads vars file from the first provider that has the app.
func (c *compositeTemplateProvider) LoadVarsFile(app string, params map[string]string) (*Vars, error) {
	for i, provider := range c.providers {
		vars, err := provider.LoadVarsFile(app, params)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading vars file for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return vars, nil
		}
		logger.Infof("Provider %d failed to load vars file for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load vars file for application '%s' from any provider", app)
}

// LoadChart loads a Helm chart from the first provider that has the app.
func (c *compositeTemplateProvider) LoadChart(app string) (chart.Charter, error) {
	for i, provider := range c.providers {
		chart, err := provider.LoadChart(app)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading chart for '%s' from custom provider\n", app, logger.VerbosityLevelDebug)
			}
			return chart, nil
		}
		logger.Infof("Provider %d failed to load chart for '%s': %v\n", i, app, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load chart for application '%s' from any provider", app)
}

// LoadYamls loads YAML files from the first provider that succeeds.
// Note: This method doesn't take an app parameter, so it uses the first provider that succeeds.
func (c *compositeTemplateProvider) LoadYamls(folder string) ([][]byte, error) {
	for i, provider := range c.providers {
		yamls, err := provider.LoadYamls(folder)
		if err == nil {
			if i > 0 {
				logger.Infof("Loading YAMLs from folder '%s' from custom provider\n", folder, logger.VerbosityLevelDebug)
			}
			return yamls, nil
		}
		logger.Infof("Provider %d failed to load YAMLs from folder '%s': %v\n", i, folder, err, logger.VerbosityLevelDebug)
	}

	return nil, fmt.Errorf("failed to load YAMLs from folder '%s' from any provider", folder)
}

// Made with Bob
