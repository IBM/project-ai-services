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

// validService returns a ServiceMetadataYAML with all required fields populated.
func validService() *bundlemetadata.ServiceMetadataYAML {
	st := true
	return &bundlemetadata.ServiceMetadataYAML{
		ID:          "my-svc",
		Type:        bundlemetadata.CatalogTypeService,
		Name:        "My Service",
		Description: "A service",
		Version:     "1.0.0",
		Standalone:  &st,
		About:       []any{"section"},
	}
}

// validComponent returns a ComponentMetadataYAML with all required fields populated.
func validComponent() *bundlemetadata.ComponentMetadataYAML {
	return &bundlemetadata.ComponentMetadataYAML{
		ID:            "my-prov",
		Type:          bundlemetadata.CatalogTypeComponent,
		Name:          "My Provider",
		Description:   "A component",
		Version:       "2.0.0",
		ComponentType: "llm",
	}
}

// serviceYAML serialises a ServiceMetadataYAML to a YAML byte slice for
// round-trip tests via ParseRootMetadata.
func serviceYAML(m *bundlemetadata.ServiceMetadataYAML) []byte {
	standalone := "false"
	if m.Standalone != nil && *m.Standalone {
		standalone = "true"
	}
	about := ""
	if len(m.About) > 0 {
		about = "\nabout:\n  - section"
	}
	return []byte("id: " + m.ID + "\ntype: " + m.Type + "\nname: " + m.Name +
		"\ndescription: " + m.Description + "\nversion: " + m.Version +
		"\nstandalone: " + standalone + about + "\n")
}

// componentYAML serialises a ComponentMetadataYAML to a YAML byte slice for
// round-trip tests via ParseRootMetadata.
func componentYAML(m *bundlemetadata.ComponentMetadataYAML) []byte {
	return []byte("id: " + m.ID + "\ntype: " + m.Type + "\nname: " + m.Name +
		"\ndescription: " + m.Description + "\nversion: " + m.Version +
		"\ncomponent_type: " + m.ComponentType + "\n")
}

// -----------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------

func TestParseRootMetadata_ServiceValid(t *testing.T) {
	result, err := bundlemetadata.ParseRootMetadata(serviceYAML(validService()))
	require.NoError(t, err)
	_, ok := result.(*bundlemetadata.ServiceMetadataYAML)
	assert.True(t, ok, "expected *ServiceMetadataYAML")
}

func TestParseRootMetadata_ComponentValid(t *testing.T) {
	result, err := bundlemetadata.ParseRootMetadata(componentYAML(validComponent()))
	require.NoError(t, err)
	_, ok := result.(*bundlemetadata.ComponentMetadataYAML)
	assert.True(t, ok, "expected *ComponentMetadataYAML")
}

func TestParseRootMetadata_ServiceStandaloneFalse(t *testing.T) {
	// standalone: false is a valid value — the pointer must be non-nil.
	m := validService()
	m.Standalone = boolPtr(false)
	_, err := bundlemetadata.ParseRootMetadata(serviceYAML(m))
	require.NoError(t, err)
}

// -----------------------------------------------------------------------
// Common required fields
// -----------------------------------------------------------------------

func TestParseRootMetadata_MissingID(t *testing.T) {
	m := validService()
	m.ID = ""
	_, err := bundlemetadata.ParseRootMetadata(serviceYAML(m))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'id' is required")
}

func TestParseRootMetadata_MissingType(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\nname: n\ndescription: d\nversion: 1.0.0\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'type' is required")
}

func TestParseRootMetadata_MissingVersion(t *testing.T) {
	m := validService()
	m.Version = ""
	_, err := bundlemetadata.ParseRootMetadata(serviceYAML(m))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

func TestParseRootMetadata_MissingName(t *testing.T) {
	m := validService()
	m.Name = ""
	_, err := bundlemetadata.ParseRootMetadata(serviceYAML(m))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestParseRootMetadata_BlankName(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\ntype: service\nname: \"   \"\ndescription: d\nversion: 1.0.0\nstandalone: true\nabout:\n  - s\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestParseRootMetadata_MissingDescription(t *testing.T) {
	m := validService()
	m.Description = ""
	_, err := bundlemetadata.ParseRootMetadata(serviceYAML(m))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'description' is required")
}

func TestParseRootMetadata_BlankDescription(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\ntype: service\nname: n\ndescription: \"   \"\nversion: 1.0.0\nstandalone: true\nabout:\n  - s\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'description' is required")
}

// -----------------------------------------------------------------------
// Service-specific
// -----------------------------------------------------------------------

func TestParseRootMetadata_ServiceMissingStandalone(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\ntype: service\nname: n\ndescription: d\nversion: 1.0.0\nabout:\n  - s\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'standalone' is required for type=service")
}

func TestParseRootMetadata_ServiceMissingAbout(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\ntype: service\nname: n\ndescription: d\nversion: 1.0.0\nstandalone: true\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'about' is required for type=service")
}

func TestParseRootMetadata_ServiceEmptyAbout(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: svc\ntype: service\nname: n\ndescription: d\nversion: 1.0.0\nstandalone: true\nabout: []\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'about' is required for type=service")
}

// -----------------------------------------------------------------------
// Component-specific
// -----------------------------------------------------------------------

func TestParseRootMetadata_ComponentMissingComponentType(t *testing.T) {
	m := validComponent()
	m.ComponentType = ""
	_, err := bundlemetadata.ParseRootMetadata(componentYAML(m))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'component_type' is required for type=component")
}

// -----------------------------------------------------------------------
// Unknown type
// -----------------------------------------------------------------------

func TestParseRootMetadata_UnknownType(t *testing.T) {
	_, err := bundlemetadata.ParseRootMetadata([]byte("id: x\ntype: architecture\nname: n\ndescription: d\nversion: 1.0.0\n"))
	assertValidationError(t, err, http.StatusUnprocessableEntity, "unsupported type")
}
