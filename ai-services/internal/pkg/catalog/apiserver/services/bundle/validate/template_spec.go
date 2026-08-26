package validate

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

// checkTemplateSpec verifies that a rendered template YAML document contains the
// required fields every ai-services resource must declare:
//
//   - apiVersion  (non-blank string)
//   - kind        (non-blank string)
//   - metadata.name                              (non-blank string)
//   - metadata.labels[ApplicationTemplateKey]   (key must be present; for Podman
//     the value is a runtime expression so only key presence is checked; for
//     OpenShift Helm renders a real value so non-empty is enforced by the caller)
//
// runtime is used as a prefix in error messages (e.g. "podman" or "openshift/templates").
// file is the template filename used in error messages.
// requireLabelValue controls whether the label value must be non-empty (true for
// OpenShift where Helm provides a real value, false for Podman where the value is
// a runtime expression that expands to "" under nil template data).
func checkTemplateSpec(doc map[string]any, runtime, file string, requireLabelValue bool) error {
	var missing []string

	if v, _ := doc["apiVersion"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "apiVersion")
	}
	if v, _ := doc["kind"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "kind")
	}

	missing = append(missing, checkMetadataFields(doc, requireLabelValue)...)

	if len(missing) > 0 {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"%s/%s: missing required Kubernetes spec field(s): %s",
				runtime, file, strings.Join(missing, ", "),
			),
		}
	}

	return nil
}

// checkMetadataFields validates metadata.name and the ai-services.io/template label.
// When the metadata block is absent both are reported missing immediately.
func checkMetadataFields(doc map[string]any, requireLabelValue bool) []string {
	templateLabelEntry := fmt.Sprintf("metadata.labels[%q]", constants.ApplicationTemplateKey)

	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		return []string{"metadata.name", templateLabelEntry}
	}

	var missing []string

	if v, _ := meta["name"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "metadata.name")
	}

	missing = append(missing, checkTemplateLabel(meta, templateLabelEntry, requireLabelValue)...)

	return missing
}

// checkTemplateLabel validates the presence (and optionally non-empty value) of
// the constants.ApplicationTemplateKey label inside a metadata map.
func checkTemplateLabel(meta map[string]any, labelEntry string, requireValue bool) []string {
	labels, _ := meta["labels"].(map[string]any)

	if requireValue {
		if v, _ := labels[constants.ApplicationTemplateKey].(string); strings.TrimSpace(v) == "" {
			return []string{labelEntry}
		}

		return nil
	}

	if _, ok := labels[constants.ApplicationTemplateKey]; !ok {
		return []string{labelEntry}
	}

	return nil
}
