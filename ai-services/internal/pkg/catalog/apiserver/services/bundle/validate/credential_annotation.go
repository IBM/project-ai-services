package validate

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/credentialspec"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// rejectCredentialAnnotation refuses a values.yaml that carries the @credential annotation.
// The annotation makes the catalog mint a service credential with the grants it names, so
// only the catalog's own assets may use it. filePath is only used in the message.
func rejectCredentialAnnotation(values []byte, filePath string) error {
	if !credentialspec.Has(values) {
		return nil
	}

	return &validators.ValidationError{
		Code: http.StatusUnprocessableEntity,
		Message: fmt.Sprintf(
			"%s: the %s annotation is reserved for built-in services and is not allowed in a bundle",
			filePath, strings.TrimSuffix(credentialspec.Marker, ":"),
		),
	}
}
