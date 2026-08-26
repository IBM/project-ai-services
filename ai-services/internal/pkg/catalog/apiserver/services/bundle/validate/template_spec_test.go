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

func TestCheckTemplateSpec_ValidPodman(t *testing.T) {
	// Podman: requireLabelValue=false, label key present with empty value (runtime expression).
	require.NoError(t, checkTemplateSpec(validDoc(""), "podman", "svc.yaml.tmpl", false))
}

func TestCheckTemplateSpec_ValidOpenShift(t *testing.T) {
	// OpenShift: requireLabelValue=true, label has a real non-empty value.
	require.NoError(t, checkTemplateSpec(validDoc("my-chart"), "openshift/templates", "svc.yaml", true))
}

// -----------------------------------------------------------------------
// Missing top-level fields
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MissingAPIVersion(t *testing.T) {
	doc := validDoc("v")
	delete(doc, "apiVersion")
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, "apiVersion")
}

func TestCheckTemplateSpec_MissingKind(t *testing.T) {
	doc := validDoc("v")
	delete(doc, "kind")
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, "kind")
}

// -----------------------------------------------------------------------
// Missing metadata fields
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MissingMetadataName(t *testing.T) {
	doc := validDoc("v")
	doc["metadata"].(map[string]any)["name"] = ""
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, "metadata.name")
}

func TestCheckTemplateSpec_MetadataBlockAbsent(t *testing.T) {
	// When metadata is nil both metadata.name and the label entry are reported.
	doc := map[string]any{"apiVersion": "v1", "kind": "Pod"}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, "metadata.name")
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

// -----------------------------------------------------------------------
// Label checks — key presence (Podman, requireLabelValue=false)
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_LabelKeyAbsent_Podman(t *testing.T) {
	doc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":   "svc",
			"labels": map[string]any{},
		},
	}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

func TestCheckTemplateSpec_LabelKeyPresentEmptyValue_Podman(t *testing.T) {
	// Podman: empty value is fine — it's a runtime expression.
	require.NoError(t, checkTemplateSpec(validDoc(""), "podman", "svc.yaml.tmpl", false))
}

func TestCheckTemplateSpec_LabelsBlockAbsent_Podman(t *testing.T) {
	doc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": "svc"},
	}
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

// -----------------------------------------------------------------------
// Label checks — non-empty value required (OpenShift, requireLabelValue=true)
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_LabelKeyAbsent_OpenShift(t *testing.T) {
	doc := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":   "svc",
			"labels": map[string]any{},
		},
	}
	err := checkTemplateSpec(doc, "openshift/templates", "svc.yaml", true)
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

func TestCheckTemplateSpec_LabelEmptyValue_OpenShift(t *testing.T) {
	// OpenShift: empty value means the label was not set by Helm — reported as missing.
	err := checkTemplateSpec(validDoc(""), "openshift/templates", "svc.yaml", true)
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

func TestCheckTemplateSpec_LabelNonEmpty_OpenShift(t *testing.T) {
	require.NoError(t, checkTemplateSpec(validDoc("my-chart"), "openshift/templates", "svc.yaml", true))
}

// -----------------------------------------------------------------------
// Multiple missing fields reported in one error
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_MultipleMissing(t *testing.T) {
	doc := map[string]any{"kind": "Pod"} // apiVersion absent, no metadata block
	err := checkTemplateSpec(doc, "podman", "svc.yaml.tmpl", false)
	assertSpecError(t, err, "apiVersion")
	assertSpecError(t, err, "metadata.name")
	assertSpecError(t, err, constants.ApplicationTemplateKey)
}

// -----------------------------------------------------------------------
// Error message format
// -----------------------------------------------------------------------

func TestCheckTemplateSpec_ErrorContainsRuntimeAndFile(t *testing.T) {
	doc := map[string]any{"apiVersion": "v1"} // kind + metadata missing
	err := checkTemplateSpec(doc, "podman", "templates/svc.yaml.tmpl", false)
	assertSpecError(t, err, "podman/templates/svc.yaml.tmpl")
}
