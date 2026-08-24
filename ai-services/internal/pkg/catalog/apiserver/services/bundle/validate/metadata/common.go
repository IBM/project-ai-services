package bundlemetadata

import (
	"net/http"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// runtimeResources is the shared resources block present in every runtime metadata.yaml.
// storage is allowed to be 0 (cloud-based components carry no local model storage).
type runtimeResources struct {
	CPU     int   `yaml:"cpu"`
	Memory  int64 `yaml:"memory"`
	Storage int64 `yaml:"storage"`
}

// validateCommonRuntimeFields checks the fields that are required in every runtime
// metadata.yaml regardless of which runtime (podman or openshift) owns the file.
// prefix is the error message prefix, e.g. "podman/metadata.yaml:".
func validateCommonRuntimeFields(name, version string, res runtimeResources, prefix string) error {
	if strings.TrimSpace(name) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: prefix + " 'name' is required"}
	}
	if strings.TrimSpace(version) == "" {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: prefix + " 'version' is required"}
	}
	if res.CPU <= 0 {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: prefix + " 'resources.cpu' must be greater than 0"}
	}
	if res.Memory <= 0 {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: prefix + " 'resources.memory' must be greater than 0"}
	}
	// storage: 0 is valid — cloud-based components have no local storage requirement.
	return nil
}
