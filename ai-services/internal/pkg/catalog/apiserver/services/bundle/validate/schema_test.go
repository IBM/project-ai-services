package validate

import (
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertSchemaError(t *testing.T, err error, msgContains string) {
	t.Helper()
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusUnprocessableEntity, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}

// -----------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------

func TestValidateValuesSchema_ValidMinimal(t *testing.T) {
	require.NoError(t, validateValuesSchema([]byte(`{"type":"object"}`), "podman/values.schema.json"))
}

func TestValidateValuesSchema_ValidWithDraft07(t *testing.T) {
	schema := `{"$schema":"https://json-schema.org/draft-07/schema#","type":"object","properties":{"name":{"type":"string"}}}`
	require.NoError(t, validateValuesSchema([]byte(schema), "podman/values.schema.json"))
}

func TestValidateValuesSchema_ValidEmptyObject(t *testing.T) {
	// An empty object {} is a valid JSON Schema (accepts everything).
	require.NoError(t, validateValuesSchema([]byte(`{}`), "openshift/values.schema.json"))
}

// -----------------------------------------------------------------------
// Invalid JSON
// -----------------------------------------------------------------------

func TestValidateValuesSchema_NotJSON(t *testing.T) {
	err := validateValuesSchema([]byte(`not json`), "podman/values.schema.json")
	assertSchemaError(t, err, "podman/values.schema.json")
	assertSchemaError(t, err, "not valid JSON")
}

func TestValidateValuesSchema_Truncated(t *testing.T) {
	err := validateValuesSchema([]byte(`{"type":`), "podman/values.schema.json")
	assertSchemaError(t, err, "not valid JSON")
}

// -----------------------------------------------------------------------
// Invalid JSON Schema keyword usage
// -----------------------------------------------------------------------

func TestValidateValuesSchema_TypeIsNumber(t *testing.T) {
	// "type" must be a string or array of strings, not a number.
	err := validateValuesSchema([]byte(`{"type": 42}`), "podman/values.schema.json")
	assertSchemaError(t, err, "podman/values.schema.json")
	assertSchemaError(t, err, "invalid JSON Schema")
}

func TestValidateValuesSchema_TypeIsInvalidString(t *testing.T) {
	// "type" must be one of the JSON Schema primitive types.
	err := validateValuesSchema([]byte(`{"type": "nonsense"}`), "podman/values.schema.json")
	assertSchemaError(t, err, "invalid JSON Schema")
}

func TestValidateValuesSchema_PropertiesNotObject(t *testing.T) {
	// "properties" must be an object, not an array.
	err := validateValuesSchema([]byte(`{"type":"object","properties":["bad"]}`), "podman/values.schema.json")
	assertSchemaError(t, err, "invalid JSON Schema")
}

// -----------------------------------------------------------------------
// Error message contains the filePath
// -----------------------------------------------------------------------

func TestValidateValuesSchema_ErrorIncludesFilePath(t *testing.T) {
	err := validateValuesSchema([]byte(`not json`), "openshift/values.schema.json")
	assertSchemaError(t, err, "openshift/values.schema.json")
}
