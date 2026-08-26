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
//   - metadata.name                           (non-blank string)
//   - metadata.labels[ApplicationTemplateKey] (key must be present; the value is
//     injected at deploy time so only key presence is enforced)
//
// runtime is used as a prefix in error messages (e.g. "podman" or "openshift/templates").
// file is the template filename used in error messages.
func checkTemplateSpec(doc map[string]any, runtime, file string) error {
	var missing []string

	if v, _ := doc["apiVersion"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "apiVersion")
	}
	if v, _ := doc["kind"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "kind")
	}

	if meta, ok := doc["metadata"].(map[string]any); !ok {
		missing = append(missing, "metadata")
	} else {
		if v, _ := meta["name"].(string); strings.TrimSpace(v) == "" {
			missing = append(missing, "metadata.name")
		}
		labels, _ := meta["labels"].(map[string]any)
		if _, ok := labels[constants.ApplicationTemplateKey]; !ok {
			missing = append(missing, fmt.Sprintf("metadata.labels[%q]", constants.ApplicationTemplateKey))
		}
	}

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
