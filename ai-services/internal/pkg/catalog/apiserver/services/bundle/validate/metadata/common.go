package bundlemetadata

import (
	"net/http"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// runtimeResources is the shared resources block present in every runtime metadata.yaml.
// Exactly four keys are permitted: cpu, memory, storage, accelerators.
// Any other key is rejected by the strict decoder used in ValidatePodmanMetadata and
// ParseAndValidateOpenShiftMetadata.
// storage is allowed to be 0 (cloud-based components carry no local model storage).
// accelerators is an optional map of resource-name → count (e.g. {"ibm.com/spyre_pf": 4});
// omitting it entirely is valid for CPU-only bundles.
type runtimeResources struct {
	CPU          int            `yaml:"cpu"`
	Memory       int64          `yaml:"memory"`
	Storage      int64          `yaml:"storage"`
	Accelerators map[string]int `yaml:"accelerators"`
}

// validateCommonRuntimeFields checks the fields that are required in every runtime
// metadata.yaml regardless of which runtime (podman or openshift) owns the file.
// prefix is the error message prefix, e.g. "podman/metadata.yaml:".
//
// Rules:
//   - name:             required, non-blank
//   - version:          required, non-blank
//   - resources.cpu:    must be > 0
//   - resources.memory: must be > 0
//   - resources.storage: must be >= 0 (0 is valid — cloud-based components have no local storage)
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
	if res.Storage < 0 {
		return &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: prefix + " 'resources.storage' must be 0 or greater"}
	}

	return nil
}
