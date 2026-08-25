package bundlemetadata

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"go.yaml.in/yaml/v3"
)

// OpenShiftMetadataYAML is the decode target for openshift/metadata.yaml.
// The OpenShift runtime currently requires only the common fields
// (name, version, resources); no runtime-specific extensions exist yet.
// Only the declared fields are accepted; unknown keys are rejected by the
// strict decoder in ParseAndValidateOpenShiftMetadata.
type OpenShiftMetadataYAML struct {
	Name      string           `yaml:"name"`
	Version   string           `yaml:"version"`
	Resources runtimeResources `yaml:"resources"`
}

// ParseAndValidateOpenShiftMetadata parses openshift/metadata.yaml, validates all
// required fields, and returns the parsed version string so the caller can perform
// a three-way version equality check (root == openshift/metadata.yaml == Chart.yaml)
// in one place.
// Unknown top-level keys or unknown resources sub-keys are rejected.
func ParseAndValidateOpenShiftMetadata(data []byte) (version string, err error) {
	var m OpenShiftMetadataYAML
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return "", &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("openshift/metadata.yaml: failed to parse: %s", err),
		}
	}
	if err := validateCommonRuntimeFields(m.Name, m.Version, m.Resources, "openshift/metadata.yaml:"); err != nil {
		return "", err
	}

	return m.Version, nil
}
