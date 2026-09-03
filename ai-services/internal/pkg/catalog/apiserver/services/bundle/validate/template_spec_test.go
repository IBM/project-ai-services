package validate

import (
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers ----------------------------------------------------------------

func assertSpecError(t *testing.T, err error, msgContains string) {
	t.Helper()
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusUnprocessableEntity, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}

// validDoc returns a fully-valid rendered doc for the given label value.
func validDoc(labelValue string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name": "svc",
			"labels": map[string]any{
				constants.ApplicationTemplateKey: labelValue,
			},
		},
	}
}

// -----------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_Valid(t *testing.T) {
	// templateID is deploy-time injected — empty value is valid for both runtimes.
	require.NoError(t, checkTemplateSpec(validDoc(""), "podman", "svc.yaml.tmpl"))
	require.NoError(t, checkTemplateSpec(validDoc(""), "openshift/templates", "svc.yaml"))
}

// -----------------------------------------------------------------------
// Missing top-level fields
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MissingAPIVersion(t *testing.T) {
	doc := validDoc("v")
	delete(doc, "apiVersion")
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, "apiVersion")
}

func TestCheckTemplateSpec_MissingKind(t *testing.T) {
	doc := validDoc("v")
	delete(doc, "kind")
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, "kind")
}

// -----------------------------------------------------------------------
// Missing metadata fields
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MissingMetadataName(t *testing.T) {
	doc := validDoc("v")
	doc["metadata"].(map[string]any)["name"] = ""
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, "metadata.name")
}

func TestCheckTemplateSpec_MetadataBlockAbsent(t *testing.T) {
	doc := map[string]any{"apiVersion": "v1", "kind": "Pod"}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, "metadata")
}

// -----------------------------------------------------------------------
// Label checks — key presence enforced for both runtimes
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_LabelKeyAbsent(t *testing.T) {
	// label map present but key missing — same code path for both runtimes.
	doc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":   "svc",
			"labels": map[string]any{},
		},
	}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

func TestCheckTemplateSpec_LabelsBlockAbsent(t *testing.T) {
	doc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": "svc"},
	}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

// -----------------------------------------------------------------------
// Multiple missing fields reported in one error
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MultipleMissing(t *testing.T) {
	doc := map[string]any{"kind": "Pod"} // apiVersion absent, no metadata block
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl")
	assertSpecError(t, err, "apiVersion")
	assertSpecError(t, err, "metadata")
}

// -----------------------------------------------------------------------
// Error message format
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_ErrorContainsRuntimeAndFile(t *testing.T) {
	doc := map[string]any{"apiVersion": "v1"} // kind + metadata missing
	err := checkTemplateSpec(doc, "podman", "templates/svc.yaml.tmpl")
	assertSpecError(t, err, "podman/templates/svc.yaml.tmpl")
}
