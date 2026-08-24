package validate_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
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
	return "id: svc\ntype: service\nversion: 1.0.0\nname: My Svc\ndescription: d\nstandalone: true\n"
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
// PodmanBundleValidator — structural and metadata checks
// -----------------------------------------------------------------------

func TestPodmanValidator_ValidMetadata(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           validPodmanMeta(),
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

func TestPodmanValidator_MetadataMissingName(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "version: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestPodmanValidator_MetadataMissingVersion(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "name: svc\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

func TestPodmanValidator_MetadataZeroCPU(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 0\n  memory: 1073741824\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.cpu' must be greater than 0")
}

func TestPodmanValidator_MetadataZeroMemory(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 0\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.memory' must be greater than 0")
}

func TestPodmanValidator_MetadataStorageZeroIsValid(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

func TestPodmanValidator_VersionMismatch(t *testing.T) {
	// podman/metadata.yaml has version 2.0.0 but root says 1.0.0
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           "name: svc\nversion: \"2.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "podman/metadata.yaml")
}

func TestPodmanValidator_MetadataWithPodTemplateExecutions(t *testing.T) {
	meta := "name: svc\nversion: \"1.0.0\"\npodTemplateExecutions:\n  - [secret.yaml.tmpl]\n  - [svc.yaml.tmpl]\nresources:\n  cpu: 2\n  memory: 2147483648\n  storage: 10737418240\n"
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           meta,
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

func TestPodmanValidator_MetadataInvalidYAML(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                  validRootMeta(),
		"podman/metadata.yaml":           ":\tinvalid yaml",
		"podman/values.yaml":             "key: value\n",
		"podman/values.schema.json":      `{"type":"object"}`,
		"podman/templates/svc.yaml.tmpl": "spec: {}\n",
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusBadRequest, "podman/metadata.yaml")
}

func TestPodmanValidator_NoPodmanDir_SkipsMetadataCheck(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": validRootMeta(),
	})

	err := validate.NewPodmanBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

// -----------------------------------------------------------------------
// OpenShiftBundleValidator — structural, metadata, and Chart.yaml checks
// -----------------------------------------------------------------------

func TestOpenShiftValidator_ValidMetadata(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                 validRootMeta(),
		"openshift/metadata.yaml":       validOpenShiftMeta(),
		"openshift/Chart.yaml":          validChart(),
		"openshift/values.yaml":         "key: value\n",
		"openshift/values.schema.json":  `{"type":"object"}`,
		"openshift/templates/svc.yaml":  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

func TestOpenShiftValidator_MetadataMissing(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/metadata.yaml")
}

func TestOpenShiftValidator_MetadataMissingName(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      "version: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestOpenShiftValidator_MetadataVersionMismatch(t *testing.T) {
	// openshift/metadata.yaml has 2.0.0 but root says 1.0.0
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      "name: svc\nversion: \"2.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n",
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/metadata.yaml")
}

func TestOpenShiftValidator_ChartVersionMismatch(t *testing.T) {
	// Chart.yaml has 2.0.0 but root says 1.0.0 (openshift/metadata.yaml matches)
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      validOpenShiftMeta(),
		"openshift/Chart.yaml":         "apiVersion: v2\nname: svc\nversion: 2.0.0\ntype: application\n",
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/Chart.yaml")
}

func TestOpenShiftValidator_MetadataZeroCPU(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      "name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 0\n  memory: 1073741824\n  storage: 0\n",
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.cpu' must be greater than 0")
}

func TestOpenShiftValidator_MetadataStorageZeroIsValid(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      validOpenShiftMeta(),
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}

func TestOpenShiftValidator_MetadataInvalidYAML(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml":                validRootMeta(),
		"openshift/metadata.yaml":      ":\tinvalid yaml",
		"openshift/Chart.yaml":         validChart(),
		"openshift/values.yaml":        "key: value\n",
		"openshift/values.schema.json": `{"type":"object"}`,
		"openshift/templates/svc.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n",
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	assertValidationError(t, err, http.StatusBadRequest, "openshift/metadata.yaml")
}

func TestOpenShiftValidator_NoOpenShiftDir_SkipsMetadataCheck(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"metadata.yaml": validRootMeta(),
	})

	err := validate.NewOpenShiftBundleValidator().Validate(archive, "", "1.0.0")
	require.NoError(t, err)
}
