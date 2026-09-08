package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// validateValuesSchema checks that raw is a valid JSON Schema document by
// compiling it with the jsonschema library. Compilation fails if:
//   - raw is not valid JSON
//   - the document is not a valid JSON Schema (e.g. wrong types for keywords)
//
// filePath is the archive-relative path used in error messages
// (e.g. "podman/values.schema.json").
func validateValuesSchema(raw []byte, filePath string) error {
	var doc any
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&doc); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("%s: not valid JSON: %s", filePath, err),
		}
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(filePath, doc); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("%s: invalid JSON Schema: %s", filePath, err),
		}
	}

	if _, err := c.Compile(filePath); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("%s: invalid JSON Schema: %s", filePath, err),
		}
	}

	return nil
}
