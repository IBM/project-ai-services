package bundlemetadata

import (
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"go.yaml.in/yaml/v3"
)

// OpenShiftMetadataYAML is the decode target for openshift/metadata.yaml.
// The OpenShift runtime currently requires only the common fields
// (name, version, resources); no runtime-specific extensions exist yet.
type OpenShiftMetadataYAML struct {
	Name      string           `yaml:"name"`
	Version   string           `yaml:"version"`
	Resources runtimeResources `yaml:"resources"`
}

// ValidateOpenShiftMetadata parses and validates openshift/metadata.yaml.
// rootVersion is the version from the root metadata.yaml; it must match the
// version declared in openshift/metadata.yaml.
func ValidateOpenShiftMetadata(data []byte, rootVersion string) error {
	var m OpenShiftMetadataYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("openshift/metadata.yaml: failed to parse: %s", err),
		}
	}
	if err := validateCommonRuntimeFields(m.Name, m.Version, m.Resources, "openshift/metadata.yaml:"); err != nil {
		return err
	}
	if m.Version != rootVersion {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"version mismatch: root metadata.yaml has %q but openshift/metadata.yaml has %q",
				rootVersion, m.Version,
			),
		}
	}

	return nil
}

// ValidateOpenShiftChartVersion parses Chart.yaml and checks that its version
// matches rootVersion.
func ValidateOpenShiftChartVersion(chartData []byte, rootVersion string) error {
	// Chart.yaml is a YAML file; we only need the version field.
	var chart struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(chartData, &chart); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("openshift/Chart.yaml: failed to parse: %s", err),
		}
	}
	if chart.Version != rootVersion {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"version mismatch: root metadata.yaml has %q but openshift/Chart.yaml has %q",
				rootVersion, chart.Version,
			),
		}
	}

	return nil
}
