package templates

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"

	"go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"

	k8syaml "sigs.k8s.io/yaml"
)

// filesystemTemplateProvider implements Template interface for filesystem-based templates.
type filesystemTemplateProvider struct {
	rootPath string
}

// NewFilesystemTemplateProvider creates a new filesystem-based template provider.
// rootPath: The root directory containing application templates.
func NewFilesystemTemplateProvider(rootPath string) (Template, error) {
	// Validate that the path exists and is a directory
	info, err := os.Stat(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("custom application path does not exist: %s", rootPath)
		}
		return nil, fmt.Errorf("failed to access custom application path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("custom application path is not a directory: %s", rootPath)
	}

	return &filesystemTemplateProvider{
		rootPath: rootPath,
	}, nil
}

// buildPath constructs a path from the root.
func (f *filesystemTemplateProvider) buildPath(parts ...string) string {
	allParts := []string{f.rootPath}
	allParts = append(allParts, parts...)
	return filepath.Join(allParts...)
}

// ListApplications lists all available application templates from filesystem.
func (f *filesystemTemplateProvider) ListApplications(hidden bool) ([]string, error) {
	apps := []string{}

	entries, err := os.ReadDir(f.rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read custom application directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		metadataPath := f.buildPath(appName, "metadata.yaml")

		// Check if metadata.yaml exists
		if _, err := os.Stat(metadataPath); err != nil {
			continue // Skip directories without metadata.yaml
		}

		md, err := f.LoadMetadata(appName, false)
		if err != nil {
			continue // Skip invalid applications
		}

		if !md.Hidden || hidden {
			apps = append(apps, appName)
		}
	}

	return apps, nil
}

// AppTemplateExist checks if the application directory exists.
func (f *filesystemTemplateProvider) AppTemplateExist(app string) error {
	appPath := f.buildPath(app, "metadata.yaml")
	if _, err := os.Stat(appPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("application template '%s' does not exist in custom path", app)
		}
		return fmt.Errorf("failed to check application template: %w", err)
	}
	return nil
}

// ListApplicationTemplateValues lists all available template value keys for a single application.
func (f *filesystemTemplateProvider) ListApplicationTemplateValues(app string) (map[string]string, error) {
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	runtimePath := f.buildPath(app, runtime)

	if _, err := os.Stat(runtimePath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("check runtime directory: %w: application %s does not support runtime %s", ErrRuntimeNotSupported, app, runtime)
		}
		return nil, fmt.Errorf("check runtime directory: %w", err)
	}

	valuesPath := filepath.Join(runtimePath, "values.yaml")
	valuesData, err := os.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("read values.yaml: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(valuesData, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml.Node: %w", err)
	}

	parametersWithDescription := make(map[string]string)
	if len(root.Content) > 0 {
		utils.FlattenNode("", root.Content[0], parametersWithDescription)
	}

	return parametersWithDescription, nil
}

// LoadAllTemplates loads all templates for a given application.
func (f *filesystemTemplateProvider) LoadAllTemplates(app string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	templatesPath := f.buildPath(app, runtime, "templates")

	err := filepath.WalkDir(templatesPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".tmpl") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read template file: %w", err)
		}

		t, err := template.New(d.Name()).Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		// Get relative path from templates directory
		relPath, err := filepath.Rel(templatesPath, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}

		tmpls[relPath] = t
		return nil
	})

	return tmpls, err
}

// LoadPodTemplate loads and renders a pod template with the given parameters.
func (f *filesystemTemplateProvider) LoadPodTemplate(app, file string, params any) (*models.PodSpec, error) {
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	path := f.buildPath(app, runtime, "templates", file)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}

	var rendered bytes.Buffer
	tmpl, err := template.New("podTemplate").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", file, err)
	}
	if err := tmpl.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to execute template %s: %v", path, err)
	}

	var spec models.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &spec); err != nil {
		return nil, fmt.Errorf("unable to read YAML as Kube Pod: %w", err)
	}

	return &spec, nil
}

// LoadPodTemplateWithValues loads and renders a pod template with values from application.
func (f *filesystemTemplateProvider) LoadPodTemplateWithValues(app, file, appName string, valuesFileOverrides []string, cliOverrides map[string]string) (*models.PodSpec, error) {
	values, err := f.LoadValues(app, valuesFileOverrides, cliOverrides)
	if err != nil {
		return nil, fmt.Errorf("failed to load params for application: %w", err)
	}

	params := map[string]any{
		"Values":          values,
		"AppName":         appName,
		"AppTemplateName": "",
		"Version":         "",
	}

	return f.LoadPodTemplate(app, file, params)
}

// LoadValues loads and merges values from default values.yaml, override files, and CLI overrides.
func (f *filesystemTemplateProvider) LoadValues(app string, valuesFileOverrides []string, cliOverrides map[string]string) (map[string]interface{}, error) {
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	valuesPath := f.buildPath(app, runtime, "values.yaml")

	valuesData, err := os.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read values.yaml: %w", err)
	}

	values := map[string]interface{}{}
	if err := yaml.Unmarshal(valuesData, &values); err != nil {
		return nil, fmt.Errorf("failed to parse values.yaml: %w", err)
	}

	// Load user provided file overrides
	for _, overridePath := range valuesFileOverrides {
		overrideData, err := os.ReadFile(overridePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read override file %s: %w", overridePath, err)
		}
		overrideValues := map[string]interface{}{}
		if err := yaml.Unmarshal(overrideData, &overrideValues); err != nil {
			return nil, fmt.Errorf("failed to parse override file %s: %w", overridePath, err)
		}

		// Validate parameters
		overrideParamsMap := utils.FlattenMapToKeys(overrideValues, "")
		if err := utils.ValidateParams(overrideParamsMap, values); err != nil {
			return nil, fmt.Errorf("validation failed for override file %s: %w", overridePath, err)
		}

		for key, val := range overrideValues {
			utils.SetNestedValue(values, key, val)
		}
	}

	// Validate and apply CLI overrides
	if err := utils.ValidateParams(cliOverrides, values); err != nil {
		return nil, err
	}

	for key, val := range cliOverrides {
		utils.SetNestedValue(values, key, val)
	}

	return values, nil
}

// LoadMetadata loads the metadata for a given application template.
func (f *filesystemTemplateProvider) LoadMetadata(app string, isRuntime bool) (*AppMetadata, error) {
	var path string
	if isRuntime {
		runtime := vars.RuntimeFactory.GetRuntimeType().String()
		path = f.buildPath(app, runtime, "metadata.yaml")
	} else {
		path = f.buildPath(app, "metadata.yaml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var appMetadata AppMetadata
	if err := yaml.Unmarshal(data, &appMetadata); err != nil {
		return nil, err
	}

	return &appMetadata, nil
}

// LoadMdFiles loads all md files for a given application.
func (f *filesystemTemplateProvider) LoadMdFiles(app string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	stepsPath := f.buildPath(app, runtime, "steps")

	err := filepath.WalkDir(stepsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read md file: %w", err)
		}

		t, err := template.New(d.Name()).Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		relPath, err := filepath.Rel(stepsPath, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}

		tmpls[relPath] = t
		return nil
	})

	return tmpls, err
}

// LoadVarsFile loads the var template file.
func (f *filesystemTemplateProvider) LoadVarsFile(app string, params map[string]string) (*Vars, error) {
	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	path := f.buildPath(app, runtime, "steps", "vars_file.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vars file: %w", err)
	}

	var rendered bytes.Buffer
	tmpl, err := template.New("varsTemplate").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", app, err)
	}
	if err := tmpl.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to execute template %s: %v", path, err)
	}

	var varsData Vars
	if err := yaml.Unmarshal(rendered.Bytes(), &varsData); err != nil {
		return nil, fmt.Errorf("unable to read YAML as vars: %w", err)
	}

	return &varsData, nil
}

// LoadChart loads a Helm chart for OpenShift runtime.
func (f *filesystemTemplateProvider) LoadChart(app string) (chart.Charter, error) {
	if vars.RuntimeFactory.GetRuntimeType() != types.RuntimeTypeOpenShift {
		return nil, errors.New("unsupported runtime type")
	}

	runtime := vars.RuntimeFactory.GetRuntimeType().String()
	chartPath := f.buildPath(app, runtime)

	return loader.Load(chartPath)
}

// LoadYamls loads YAML files from a folder for OpenShift runtime.
func (f *filesystemTemplateProvider) LoadYamls(folder string) ([][]byte, error) {
	if vars.RuntimeFactory.GetRuntimeType() != types.RuntimeTypeOpenShift {
		return nil, errors.New("unsupported runtime type")
	}

	var yamls [][]byte
	runtime := vars.RuntimeFactory.GetRuntimeType().String()

	searchRoot := f.buildPath(runtime)
	if folder != "" {
		searchRoot = f.buildPath(runtime, folder)
	}

	err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		ext := filepath.Ext(d.Name())
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		yamlData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("error reading %s: %w", path, err)
		}

		yamls = append(yamls, yamlData)
		return nil
	})

	return yamls, err
}

// Made with Bob
