package templates

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"

	"go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	k8syaml "sigs.k8s.io/yaml"
)

const (
	/*
		Templates Pattern :- "applications/<AppName>/metadata.yaml"
		After splitting, the application name is located at second part.
		So we ensure the path contains enough segments which is appName index + 1.
	*/
	minPathPartsForAppName = 3
)

// ErrRuntimeNotSupported is returned when an application does not support the requested runtime.
var ErrRuntimeNotSupported = errors.New("runtime not supported")

type embedTemplateProvider struct {
	fs   *embed.FS
	root string
}

// NewEmbedTemplateProvider creates a new template provider.
// fs: The embed.FS to use (e.g., &assets.ApplicationFS, &assets.BootstrapFS, &assets.CatalogFS)
// root: (optional) Custom root directory path within the embed.FS. If not provided, defaults are used based on the fs type.
func NewEmbedTemplateProvider(fs *embed.FS, root ...string) Template {
	var rootPath string

	// Use custom root if provided
	if len(root) == 1 {
		rootPath = root[0]
	} else if len(root) > 1 {
		rootPath = strings.Join(root, "/")
	} else {
		// Determine default based on fs
		switch fs {
		case &assets.BootstrapFS:
			rootPath = "bootstrap"
		case &assets.CatalogFS:
			rootPath = "catalog"
		default:
			rootPath = "applications"
		}
	}

	return &embedTemplateProvider{
		fs:   fs,
		root: rootPath,
	}
}

func getRuntime() string {
	return vars.RuntimeFactory.GetRuntimeType().String()
}

// buildPath constructs a path handling empty root correctly.
func (e *embedTemplateProvider) buildPath(parts ...string) string {
	allParts := []string{}
	if e.root != "" {
		allParts = append(allParts, e.root)
	}
	allParts = append(allParts, parts...)

	return strings.Join(allParts, "/")
}

// ListApplications lists all available application templates.
func (e *embedTemplateProvider) ListApplications(hidden bool) ([]string, error) {
	apps := []string{}

	err := fs.WalkDir(e.fs, e.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Templates Pattern :- "applications/<AppName>/metadata.yaml" (Top level metadata file)
		parts := strings.Split(filepath.ToSlash(path), "/")
		if len(parts) == minPathPartsForAppName && filepath.Base(path) == "metadata.yaml" {
			appName := parts[1]
			md, err := e.LoadMetadata(appName, false)
			if err != nil {
				return err
			}
			if !md.Hidden || hidden {
				apps = append(apps, appName)
			}

			return fs.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return apps, nil
}

// AppTemplateExist Check if the application directory exists.
func (e *embedTemplateProvider) AppTemplateExist(app string) error {
	appPath := e.buildPath(app, "metadata.yaml")
	_, err := fs.Stat(e.fs, appPath)
	if err != nil {
		return fmt.Errorf("application template '%s' does not exist", app)
	}

	return nil
}

// ListApplicationTemplateValues lists all available template value keys for a single application.
func (e *embedTemplateProvider) ListApplicationTemplateValues(app string) (map[string]string, error) {
	// Check if the runtime directory exists for this application
	runtimePath := e.buildPath(app, getRuntime())
	_, err := fs.Stat(e.fs, runtimePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("check runtime directory: %w: application %s does not support runtime %s", ErrRuntimeNotSupported, app, getRuntime())
		}

		return nil, fmt.Errorf("check runtime directory: %w", err)
	}

	valuesPath := fmt.Sprintf("%s/values.yaml", runtimePath)
	valuesData, err := e.fs.ReadFile(valuesPath)
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
func (e *embedTemplateProvider) LoadAllTemplates(app string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	completePath := e.buildPath(app, getRuntime(), "templates")
	err := fs.WalkDir(e.fs, completePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".tmpl") {
			return nil
		}

		t, err := template.ParseFS(e.fs, path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		// key should be just the template file name (Eg:- pod1.yaml.tmpl)
		tmpls[strings.TrimPrefix(path, fmt.Sprintf("%s/", completePath))] = t

		return nil
	})

	return tmpls, err
}

// LoadPodTemplate loads and renders a pod template with the given parameters.
func (e *embedTemplateProvider) LoadPodTemplate(app, file string, params any) (*models.PodSpec, error) {
	path := e.buildPath(app, getRuntime(), "templates", file)
	data, err := e.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
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

func (e *embedTemplateProvider) LoadPodTemplateWithValues(app, file, appName string, valuesFileOverrides []string, cliOverrides map[string]string) (*models.PodSpec, error) {
	values, err := e.LoadValues(app, valuesFileOverrides, cliOverrides)
	if err != nil {
		return nil, fmt.Errorf("failed to load params for application: %w", err)
	}
	// Build full params directly
	params := map[string]any{
		"Values":          values,
		"AppName":         appName,
		"AppTemplateName": "",
		"Version":         "",
	}

	return e.LoadPodTemplate(app, file, params)
}

func (e *embedTemplateProvider) LoadValues(app string, valuesFileOverrides []string, cliOverrides map[string]string) (map[string]interface{}, error) {
	// Load the default values.yaml
	valuesPath := e.buildPath(app, getRuntime(), "values.yaml")
	valuesData, err := e.fs.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read values.yaml: %w", err)
	}
	values := map[string]interface{}{}
	if err := yaml.Unmarshal(valuesData, &values); err != nil {
		return nil, fmt.Errorf("failed to parse values.yaml: %w", err)
	}

	// Load user provided file overrides and validate them
	for _, overridePath := range valuesFileOverrides {
		overrideData, err := os.ReadFile(overridePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read override file %s: %w", overridePath, err)
		}
		overrideValues := map[string]interface{}{}
		if err := yaml.Unmarshal(overrideData, &overrideValues); err != nil {
			return nil, fmt.Errorf("failed to parse override file %s: %w", overridePath, err)
		}

		// Validate that all parameters in the override file are supported
		overrideParamsMap := utils.FlattenMapToKeys(overrideValues, "")
		if err := utils.ValidateParams(overrideParamsMap, values); err != nil {
			return nil, fmt.Errorf("validation failed for override file %s: %w", overridePath, err)
		}

		for key, val := range overrideValues {
			utils.SetNestedValue(values, key, val)
		}
	}

	// validate CLI Overrides before applying since we are adding them directly
	if err := utils.ValidateParams(cliOverrides, values); err != nil {
		return nil, err
	}

	// Load user provided CLI overides
	for key, val := range cliOverrides {
		utils.SetNestedValue(values, key, val)
	}

	return values, nil
}

// LoadMetadata loads the metadata for a given application template.
// if runtime is empty then it loads the app Metadata.
// if set it loads the runtime specific metadata.
func (e *embedTemplateProvider) LoadMetadata(app string, isRuntime bool) (*AppMetadata, error) {
	// construct metadata.yaml path
	var p string
	if isRuntime {
		p = e.buildPath(app, getRuntime(), "metadata.yaml")
	} else {
		p = e.buildPath(app, "metadata.yaml")
	}

	data, err := e.fs.ReadFile(p)
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
func (e *embedTemplateProvider) LoadMdFiles(app string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	completePath := e.buildPath(app, getRuntime(), "steps")
	err := fs.WalkDir(e.fs, completePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		t, err := template.ParseFS(e.fs, path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		// key should be just the template file name (Eg:- pod1.yaml.tmpl)
		tmpls[strings.TrimPrefix(path, fmt.Sprintf("%s/", completePath))] = t

		return nil
	})

	return tmpls, err
}

func (e *embedTemplateProvider) LoadVarsFile(app string, params map[string]string) (*Vars, error) {
	path := e.buildPath(app, getRuntime(), "steps", "vars_file.yaml")

	data, err := e.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var rendered bytes.Buffer
	tmpl, err := template.New("varsTemplate").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", app, err)
	}
	if err := tmpl.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to execute template %s: %v", path, err)
	}

	var vars Vars
	if err := yaml.Unmarshal(data, &vars); err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(rendered.Bytes(), &vars); err != nil {
		return nil, fmt.Errorf("unable to read YAML as vars Pod: %w", err)
	}

	return &vars, nil
}

func (e *embedTemplateProvider) LoadChart(app string) (chart.Charter, error) {
	if getRuntime() != string(types.RuntimeTypeOpenShift) {
		return nil, errors.New("unsupported runtime type")
	}

	// construct chart path
	chartPath := e.buildPath(app, getRuntime())

	var files []*archive.BufferedFile
	err := fs.WalkDir(e.fs, chartPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := e.fs.ReadFile(p)
		if err != nil {
			return err
		}

		// Make file name relative to chart root for helm loader
		rel := strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(chartPath)+"/")

		files = append(files, &archive.BufferedFile{
			Name: rel,
			Data: data,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return loader.LoadFiles(files)
}

func (e *embedTemplateProvider) LoadYamls(folder string) ([][]byte, error) {
	if getRuntime() != string(types.RuntimeTypeOpenShift) {
		return nil, errors.New("unsupported runtime type")
	}
	var yamls [][]byte

	searchRoot := e.buildPath(getRuntime())
	if folder != "" {
		searchRoot = e.buildPath(getRuntime(), folder)
	}

	err := fs.WalkDir(e.fs, searchRoot, func(p string, d fs.DirEntry, err error) error {
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

		yaml, err := fs.ReadFile(e.fs, p)
		if err != nil {
			return fmt.Errorf("error reading %p: %w", yaml, err)
		}

		yamls = append(yamls, yaml)

		return nil
	})

	return yamls, err
}
