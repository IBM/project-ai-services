package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"go.yaml.in/yaml/v3"
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

type embedTemplateProvider struct {
	fs      *embed.FS
	root    string
	runtime types.RuntimeType
}

func (e *embedTemplateProvider) Runtime() string {
	return e.runtime.String()
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

// ListApplicationTemplateValues lists all available template value keys for a single application.
func (e *embedTemplateProvider) ListApplicationTemplateValues(app string) (map[string]string, error) {
	valuesPath := fmt.Sprintf("%s/%s/%s/values.yaml", e.root, app, e.Runtime())
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
	completePath := fmt.Sprintf("%s/%s/%s/templates", e.root, app, e.Runtime())
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
	path := fmt.Sprintf("%s/%s/%s/templates/%s", e.root, app, e.Runtime(), file)
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
	valuesPath := fmt.Sprintf("%s/%s/%s/values.yaml", e.root, app, e.Runtime())
	valuesData, err := e.fs.ReadFile(valuesPath)
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
	p := path.Join(e.root, app)
	if isRuntime {
		p = path.Join(p, e.Runtime())
	}
	p = path.Join(p, "metadata.yaml")

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
	completePath := fmt.Sprintf("%s/%s/%s/steps", e.root, app, e.Runtime())
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
	path := fmt.Sprintf("%s/%s/%s/steps/vars_file.yaml", e.root, app, e.Runtime())

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

type EmbedOptions struct {
	FS      *embed.FS
	Root    string
	Runtime types.RuntimeType
}

// NewEmbedTemplateProvider creates a new instance of embedTemplateProvider.
func NewEmbedTemplateProvider(options EmbedOptions) Template {
	t := &embedTemplateProvider{}
	if options.FS != nil {
		t.fs = options.FS
	} else {
		t.fs = &assets.ApplicationFS
	}

	if options.Root != "" {
		t.root = options.Root
	} else {
		t.root = "applications"
	}

	// Use Podman runtime if not set by default
	t.runtime = types.RuntimeTypePodman
	if options.Runtime != "" {
		t.runtime = options.Runtime
	}

	return t
}
