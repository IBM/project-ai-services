package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"go.yaml.in/yaml/v3"
	k8syaml "sigs.k8s.io/yaml"
)

type embedTemplateProvider struct {
	fs   *embed.FS
	root string
}

// ListApplications lists all available application templates
func (e *embedTemplateProvider) ListApplications() ([]string, error) {
	apps := []string{}

	err := fs.WalkDir(e.fs, e.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Templates Pattern :- "assets/applications/<AppName>/templates/*.yaml.tmpl"
		parts := strings.Split(path, "/")

		if len(parts) >= 4 {
			appName := parts[1]
			if slices.Contains(apps, appName) {
				return nil
			}
			apps = append(apps, appName)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return apps, nil
}

// ListApplicationTemplateValues lists all available template value keys for a single application.
func (e *embedTemplateProvider) ListApplicationTemplateValues(app string) ([]string, error) {
	valuesPath := fmt.Sprintf("%s/%s/values.yaml", e.root, app)
	valuesData, err := e.fs.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("read values.yaml: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(valuesData, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml.Node: %w", err)
	}

	var keys []string
	if len(root.Content) > 0 {
		utils.FlattenNode("", root.Content[0], &keys)
	}

	sort.Strings(keys)
	return keys, nil
}

// LoadAllTemplates loads all templates for a given application
func (e *embedTemplateProvider) LoadAllTemplates(path string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	completePath := fmt.Sprintf("%s/%s", e.root, path)
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

// LoadPodTemplate loads and renders a pod template with the given parameters
func (e *embedTemplateProvider) LoadPodTemplate(app, file string, params any) (*models.PodSpec, error) {
	path := fmt.Sprintf("%s/%s/templates/%s", e.root, app, file)
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

func (e *embedTemplateProvider) LoadPodTemplateWithValues(app, file, appName string, overrides map[string]string) (*models.PodSpec, error) {
	values, err := e.LoadValues(app, overrides)
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

func (e *embedTemplateProvider) LoadValues(app string, overrides map[string]string) (map[string]interface{}, error) {
	valuesPath := fmt.Sprintf("%s/%s/values.yaml", e.root, app)
	valuesData, err := e.fs.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("read values.yaml: %w", err)
	}
	values := map[string]interface{}{}
	if err := yaml.Unmarshal(valuesData, &values); err != nil {
		return nil, fmt.Errorf("parse values.yaml: %w", err)
	}
	for key, val := range overrides {
		utils.SetNestedValue(values, key, val)
	}
	return values, nil
}

// LoadMetadata loads the metadata for a given application template
func (e *embedTemplateProvider) LoadMetadata(appTemplateName string) (*AppMetadata, error) {
	path := fmt.Sprintf("%s/%s/metadata.yaml", e.root, appTemplateName)
	data, err := e.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var appMetadata AppMetadata
	if err := yaml.Unmarshal(data, &appMetadata); err != nil {
		return nil, err
	}
	return &appMetadata, nil
}

// LoadMdFiles loads all md files for a given application
func (e *embedTemplateProvider) LoadMdFiles(path string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)
	completePath := fmt.Sprintf("%s/%s", e.root, path)
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
	path := fmt.Sprintf("%s/%s/steps/vars_file.yaml", e.root, app)

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
	FS   *embed.FS
	Root string
}

// NewEmbedTemplateProvider creates a new instance of embedTemplateProvider
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
	return t
}
