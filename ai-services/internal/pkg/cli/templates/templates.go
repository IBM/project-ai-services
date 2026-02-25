package templates

import (
	"text/template"
	"time"

	"helm.sh/helm/v4/pkg/chart"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
)

type AppMetadata struct {
	Name                  string           `yaml:"name,omitempty"`
	Description           string           `yaml:"description,omitempty"`
	Hidden                bool             `yaml:"hidden,omitempty"`
	Version               string           `yaml:"version,omitempty"`
	SMTLevel              *int             `yaml:"smtLevel,omitempty"`
	PodTemplateExecutions [][]string       `yaml:"podTemplateExecutions"`
	Openshift             OpenshiftRuntime `yaml:"openshift,omitempty"`
}

type OpenshiftRuntime struct {
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

type Vars struct {
	Pods       []PodVar       `yaml:"pods,omitempty"`
	Containers []ContainerVar `yaml:"containers,omitempty"`
	Hosts      []HostVar      `yaml:"hosts,omitempty"`
}

type PodVar struct {
	Name    string  `yaml:"name,omitempty"`
	Format  string  `yaml:"format,omitempty"`
	Default *string `yaml:"default,omitempty"`
	Alias   string  `yaml:"alias,omitempty"`
}

type ContainerVar struct {
	Name    string  `yaml:"name,omitempty"`
	Format  string  `yaml:"format,omitempty"`
	Default *string `yaml:"default,omitempty"`
	Alias   string  `yaml:"alias,omitempty"`
}

type HostVar struct {
	Fetch string `yaml:"fetch,omitempty"`
	Type  string `yaml:"type,omitempty"`
}

type Template interface {
	// ListApplications lists all available application templates
	ListApplications(hidden bool) ([]string, error)
	// ListApplicationTemplateValues lists all available template parameters with description for a single application.
	ListApplicationTemplateValues(app string) (map[string]string, error)
	// LoadAllTemplates loads all templates for a given application
	LoadAllTemplates(app string) (map[string]*template.Template, error)
	// LoadPodTemplate loads and renders a pod template with the given parameters
	LoadPodTemplate(app, file string, params any) (*models.PodSpec, error)
	// LoadPodTemplateWithValues loads and renders a pod template with values from application
	LoadPodTemplateWithValues(app, file, appName string, valuesFileOverrides []string, cliOverrides map[string]string) (*models.PodSpec, error)
	LoadValues(app string, valuesFileOverrides []string, cliOverrides map[string]string) (map[string]interface{}, error)
	// LoadMetadata loads the metadata for a given application template
	LoadMetadata(app string, isRuntime bool) (*AppMetadata, error)
	// LoadMdFiles loads all md files for a given application
	LoadMdFiles(app string) (map[string]*template.Template, error)
	// LoadVarsFile loads the var template file
	LoadVarsFile(app string, params map[string]string) (*Vars, error)
	// LoadVarsFile loads the Chart
	LoadChart(app string) (chart.Charter, error)
	// LoadYamls loads the yaml in assests dir
	LoadYamls() ([][]byte, error)
}
