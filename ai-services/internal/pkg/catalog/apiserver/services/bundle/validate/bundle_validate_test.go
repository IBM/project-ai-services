package validate_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Archive builder
// -----------------------------------------------------------------------

// buildArchive creates a gzip-compressed tar archive in memory.
// entries maps relative path → file content, all placed under a "bundle/" top-level dir.
func buildArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "bundle/", Typeflag: tar.TypeDir, Mode: 0o755}))

	for name, content := range entries {
		body := []byte(content)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     "bundle/" + name,
			Typeflag: tar.TypeReg,
			Size:     int64(len(body)),
			Mode:     0o644,
		}))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func assertValidationError(t *testing.T, err error, code int, msgContains string) {
	t.Helper()
	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	if msgContains != "" {
		assert.Contains(t, valErr.Message, msgContains)
	}
}

// validRootMeta returns a minimal valid root metadata.yaml for a service bundle.
func validRootMeta() string {
	return "id: svc\ntype: service\nversion: 1.0.0\nname: My Svc\ndescription: d\nstandalone: true\nabout:\n  - section\n"
}

// validPodmanMeta returns a minimal valid podman/metadata.yaml.
func validPodmanMeta() string {
	return "name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n"
}

// validOpenShiftMeta returns a minimal valid openshift/metadata.yaml.
func validOpenShiftMeta() string {
	return validPodmanMeta() // same schema
}

// validChart returns a minimal valid Chart.yaml for the Helm validator.
func validChart() string {
	return "apiVersion: v2\nname: svc\nversion: 1.0.0\ntype: application\n"
}

// -----------------------------------------------------------------------
// PodmanBundleValidator
// -----------------------------------------------------------------------

// validPodmanTemplate returns a minimal valid podman *.yaml.tmpl that satisfies
// the spec check (apiVersion, kind, metadata.name, ApplicationTemplateKey label).
func validPodmanTemplate() string {
	return fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata:\n  name: svc\n  labels:\n    %s: \"{{ .TemplateID }}\"\nspec: {}\n", constants.ApplicationTemplateKey)
}

func TestPodmanValidator_ValidBundle(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           validPodmanMeta(),
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"$schema":"https://json-schema.org/draft-07/schema#","type":"object"}`,
		"podman/templates/svc.yaml.tmpl": validPodmanTemplate(),
	})
	require.NoError(t, validate.NewPodmanBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

func TestPodmanValidator_NoPodmanDir_Skips(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": validRootMeta(),
	})
	require.NoError(t, validate.NewPodmanBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

func TestPodmanValidator_MetadataWithPodTemplateExecutions(t *testing.T) {
	meta := "name: svc\nversion: \"1.0.0\"\npodTemplateExecutions:\n  - [secret.yaml.tmpl]\n  - [svc.yaml.tmpl]\nresources:\n  cpu: 2\n  memory: 2147483648\n  storage: 10737418240\n"
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           meta,
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"$schema":"https://json-schema.org/draft-07/schema#","type":"object"}`,
		"podman/templates/svc.yaml.tmpl": validPodmanTemplate(),
	})
	require.NoError(t, validate.NewPodmanBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

// -----------------------------------------------------------------------
// Podman template pipeline (Go template parse/execute — Podman-specific)
// -----------------------------------------------------------------------

func TestPodmanValidator_TemplateSyntaxError(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           validPodmanMeta(),
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "apiVersion: v1\nkind: Pod\nmetadata:\n  name: {{ .Broken\n",
	})
	err := validate.NewPodmanBundleValidator().Validate(archive, "bundle", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "template syntax error")
}

func TestPodmanValidator_TemplateWithValuesRendersValid(t *testing.T) {
	// Template uses runtime expressions — those expand to "" under nil data,
	// but the structural keys including the label are still present.
	tmpl := fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata:\n  name: {{ .AppName }}--svc\n  labels:\n    %s: \"{{ .TemplateID }}\"\nspec: {}\n", constants.ApplicationTemplateKey)
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           validPodmanMeta(),
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": tmpl,
	})
	require.NoError(t, validate.NewPodmanBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

// -----------------------------------------------------------------------
// OpenShiftBundleValidator
// -----------------------------------------------------------------------

func TestOpenShiftValidator_ValidBundle(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      validOpenShiftMeta(),
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n  labels:\n    %s: {{ .Chart.Name }}\n", constants.ApplicationTemplateKey),
	})
	require.NoError(t, validate.NewOpenShiftBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

func TestOpenShiftValidator_TemplateMissingAIServicesLabel(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      validOpenShiftMeta(),
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})
	err := validate.NewOpenShiftBundleValidator().Validate(archive, "bundle", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, constants.ApplicationTemplateKey)
}

func TestOpenShiftValidator_NoOpenShiftDir_Skips(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": validRootMeta(),
	})
	require.NoError(t, validate.NewOpenShiftBundleValidator().Validate(archive, "bundle", "1.0.0"))
}

// TestOpenShiftValidator_MetadataMissing exercises the structural path check —
// the openshift/ directory is present but metadata.yaml is absent.
func TestOpenShiftValidator_MetadataMissing(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})
	err := validate.NewOpenShiftBundleValidator().Validate(archive, "bundle", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/metadata.yaml")
}

// TestOpenShiftValidator_ChartVersionMismatch is the only version-mismatch test kept
// at the integration level because the Chart.yaml version is read by the Helm loader
// (not by a standalone YAML parse) and has no unit-level equivalent.
func TestOpenShiftValidator_ChartVersionMismatch(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      validOpenShiftMeta(),
		"openshift/Chart.yaml":         "apiVersion: v2\nname: svc\nversion: 2.0.0\ntype: application\n",
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n  labels:\n    %s: {{ .Chart.Name }}\n", constants.ApplicationTemplateKey),
	})
	err := validate.NewOpenShiftBundleValidator().Validate(archive, "bundle", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/Chart.yaml")
}
