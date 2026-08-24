package bundlemetadata_test

import (
	"net/http"
	"testing"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertValidationError is a helper that asserts err is a *validators.ValidationError
// with the given HTTP code and, when msgContains is non-empty, checks the message.
func assertValidationError(t *testing.T, err error, code int, msgContains string) {
	t.Helper()
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	if msgContains != "" {
		assert.Contains(t, valErr.Message, msgContains)
	}
}

// boolPtr returns a pointer to the given bool — used to set the Standalone field.
func boolPtr(b bool) *bool { return &b }

// validService returns a MetadataYAML with all service fields populated.
func validService() *bundlemetadata.MetadataYAML {
	st := true
	return &bundlemetadata.MetadataYAML{
		ID:          "my-svc",
		Type:        bundlemetadata.CatalogTypeService,
		Name:        "My Service",
		Description: "A service",
		Version:     "1.0.0",
		Standalone:  &st,
	}
}

// validComponent returns a MetadataYAML with all component fields populated.
func validComponent() *bundlemetadata.MetadataYAML {
	return &bundlemetadata.MetadataYAML{
		ID:            "my-prov",
		Type:          bundlemetadata.CatalogTypeComponent,
		Name:          "My Provider",
		Description:   "A component",
		Version:       "2.0.0",
		ComponentType: "llm",
	}
}

// -----------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------

func TestValidateRootMetadata_ServiceValid(t *testing.T) {
	require.NoError(t, bundlemetadata.ValidateRootMetadata(validService()))
}

func TestValidateRootMetadata_ComponentValid(t *testing.T) {
	require.NoError(t, bundlemetadata.ValidateRootMetadata(validComponent()))
}

func TestValidateRootMetadata_ServiceStandaloneFalse(t *testing.T) {
	// standalone: false is a valid value — the pointer must be non-nil.
	m := validService()
	m.Standalone = boolPtr(false)
	require.NoError(t, bundlemetadata.ValidateRootMetadata(m))
}

// -----------------------------------------------------------------------
// Common required fields
// -----------------------------------------------------------------------

func TestValidateRootMetadata_MissingID(t *testing.T) {
	m := validService()
	m.ID = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'id' is required")
}

func TestValidateRootMetadata_MissingType(t *testing.T) {
	m := validService()
	m.Type = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'type' is required")
}

func TestValidateRootMetadata_MissingVersion(t *testing.T) {
	m := validService()
	m.Version = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'version' is required")
}

func TestValidateRootMetadata_MissingName(t *testing.T) {
	m := validService()
	m.Name = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'name' is required")
}

func TestValidateRootMetadata_BlankName(t *testing.T) {
	m := validService()
	m.Name = "   "
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'name' is required")
}

func TestValidateRootMetadata_MissingDescription(t *testing.T) {
	m := validService()
	m.Description = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'description' is required")
}

func TestValidateRootMetadata_BlankDescription(t *testing.T) {
	m := validService()
	m.Description = "   "
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'description' is required")
}

// -----------------------------------------------------------------------
// Service-specific
// -----------------------------------------------------------------------

func TestValidateRootMetadata_ServiceMissingStandalone(t *testing.T) {
	m := validService()
	m.Standalone = nil
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'standalone' is required for type=service")
}

// -----------------------------------------------------------------------
// Component-specific
// -----------------------------------------------------------------------

func TestValidateRootMetadata_ComponentMissingComponentType(t *testing.T) {
	m := validComponent()
	m.ComponentType = ""
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "'component_type' is required for type=component")
}

// -----------------------------------------------------------------------
// Unknown type
// -----------------------------------------------------------------------

func TestValidateRootMetadata_UnknownType(t *testing.T) {
	m := validService()
	m.Type = "architecture"
	assertValidationError(t, bundlemetadata.ValidateRootMetadata(m), http.StatusUnprocessableEntity, "unsupported type")
}
