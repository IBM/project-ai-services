package bundlemetadata

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"go.yaml.in/yaml/v3"
)

// PodmanMetadataYAML is the decode target for podman/metadata.yaml.
// It extends the common required fields (name, version, resources) with
// podman-specific options.
// Only the declared fields are accepted; unknown keys are rejected by the
// strict decoder in ValidatePodmanMetadata.
type PodmanMetadataYAML struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	// PodTemplateExecs is optional. When present each inner slice lists the
	// template filenames to execute in order for a single pod. An absent or
	// empty value is valid — the runtime will fall back to executing all
	// templates/*.yaml.tmpl files in lexicographic order.
	PodTemplateExecs [][]string       `yaml:"podTemplateExecutions"`
	Resources        runtimeResources `yaml:"resources"`
}

// ValidatePodmanMetadata parses and validates podman/metadata.yaml.
// rootVersion is the version from the root metadata.yaml; it must match the
// version declared in podman/metadata.yaml.
// Unknown top-level keys or unknown resources sub-keys are rejected.
func ValidatePodmanMetadata(data []byte, rootVersion string) error {
	var m PodmanMetadataYAML
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("podman/metadata.yaml: failed to parse: %s", err),
		}
	}
	if err := validateCommonRuntimeFields(m.Name, m.Version, m.Resources, "podman/metadata.yaml:"); err != nil {
		return err
	}
	if m.Version != rootVersion {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"version mismatch: root metadata.yaml has %q but podman/metadata.yaml has %q",
				rootVersion, m.Version,
			),
		}
	}

	return nil
}
