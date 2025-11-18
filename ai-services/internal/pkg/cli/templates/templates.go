package templates

import (
	"text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
)

type AppMetadata struct {
	Name                  string     `yaml:"name,omitempty"`
	Version               string     `yaml:"version,omitempty"`
	SMTLevel              *int       `yaml:"smtLevel,omitempty"`
	PodTemplateExecutions [][]string `yaml:"podTemplateExecutions"`
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
	LoadPodTemplate(app, file string, params any) (*models.PodSpec, error)
	// LoadPodTemplateWithDummyParams loads and renders a pod template with the dummy parameters
	LoadPodTemplateWithDummyParams(app, file string) (*models.PodSpec, error)
	// LoadMetadata loads the metadata for a given application template
	LoadMetadata(app string) (*AppMetadata, error)
	// LoadMdFiles loads all md files for a given application
	LoadMdFiles(path string) (map[string]*template.Template, error)
	// LoadVarsFile loads the var template file
	LoadVarsFile(app string, params map[string]string) (*Vars, error)
}
