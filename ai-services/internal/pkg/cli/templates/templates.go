package templates

import (
	"text/template"

	v1 "github.com/containers/podman/v5/pkg/k8s.io/api/core/v1"
)

type AppMetadata struct {
	// TODO: Include other variables too
	SMTLevel              *int       `yaml:"smtLevel,omitempty"`
	PodTemplateExecutions [][]string `yaml:"podTemplateExecutions"`
}

type PodSpec struct {
	v1.Pod
}

type Vars struct {
	Pods  []PodVar  `yaml:"pods,omitempty"`
	Hosts []HostVar `yaml:"hosts,omitempty"`
}

type PodVar struct {
	Name  string `yaml:"name,omitempty"`
	Fetch string `yaml:"fetch,omitempty"`
	Type  string `yaml:"type,omitempty"`
}

type HostVar struct {
	Fetch string `yaml:"fetch,omitempty"`
	Type  string `yaml:"type,omitempty"`
}

type Template interface {
	// ListApplications lists all available application templates
	ListApplications() ([]string, error)
	// LoadAllTemplates loads all templates for a given application
	LoadAllTemplates(path string) (map[string]*template.Template, error)
	// LoadPodTemplate loads and renders a pod template with the given parameters
	LoadPodTemplate(app, file string, params any) (*PodSpec, error)
	// LoadMetadata loads the metadata for a given application template
	LoadMetadata(app string) (*AppMetadata, error)
	// LoadMdFiles loads all md files for a given application
	LoadMdFiles(path string) (map[string]*template.Template, error)
	// LoadVarsFile loads the var template file
	LoadVarsFile(app string, params map[string]string) (*Vars, error)
}
