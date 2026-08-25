package bundlemetadata_test

import (
	"net/http"
	"testing"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// ValidatePodmanMetadata — version cross-check
// -----------------------------------------------------------------------

func validPodmanMeta(version string) []byte {
	return []byte("name: svc\nversion: " + version + "\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n")
}

func TestValidatePodmanMetadata_VersionMatch(t *testing.T) {
	require.NoError(t, bundlemetadata.ValidatePodmanMetadata(validPodmanMeta(`"1.0.0"`), "1.0.0"))
}

func TestValidatePodmanMetadata_VersionMismatch(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(validPodmanMeta(`"2.0.0"`), "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "podman/metadata.yaml")
}

func TestValidatePodmanMetadata_InvalidYAML(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata([]byte(":\tinvalid"), "1.0.0")
	assertValidationError(t, err, http.StatusBadRequest, "podman/metadata.yaml")
}

func TestValidatePodmanMetadata_MissingFields(t *testing.T) {
	// version field absent — validateCommonRuntimeFields fires before version cross-check.
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nresources:\n  cpu: 1\n  memory: 1073741824\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

// -----------------------------------------------------------------------
// ParseAndValidateOpenShiftMetadata — field validation and version return
// -----------------------------------------------------------------------

func validOpenShiftMeta(version string) []byte {
	return []byte("name: svc\nversion: " + version + "\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n")
}

func TestParseAndValidateOpenShiftMetadata_ReturnsVersion(t *testing.T) {
	ver, err := bundlemetadata.ParseAndValidateOpenShiftMetadata(validOpenShiftMeta(`"1.0.0"`))
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", ver)
}

func TestParseAndValidateOpenShiftMetadata_MissingFields(t *testing.T) {
	_, err := bundlemetadata.ParseAndValidateOpenShiftMetadata(
		[]byte("name: svc\nresources:\n  cpu: 1\n  memory: 1073741824\n"),
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

func TestParseAndValidateOpenShiftMetadata_InvalidYAML(t *testing.T) {
	_, err := bundlemetadata.ParseAndValidateOpenShiftMetadata([]byte(":\tinvalid yaml"))
	assertValidationError(t, err, http.StatusBadRequest, "openshift/metadata.yaml")
}

// -----------------------------------------------------------------------
// validateCommonRuntimeFields — field-level coverage via ValidatePodmanMetadata
// (OpenShift uses the same helper; one set of field tests is sufficient)
// -----------------------------------------------------------------------

func TestValidatePodmanMetadata_MissingName(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("version: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestValidatePodmanMetadata_BlankName(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: \"   \"\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'name' is required")
}

func TestValidatePodmanMetadata_MissingCPU(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 0\n  memory: 1073741824\n  storage: 0\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.cpu' must be greater than 0")
}

func TestValidatePodmanMetadata_MissingMemory(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 0\n  storage: 0\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.memory' must be greater than 0")
}

func TestValidatePodmanMetadata_NegativeStorage(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: -1\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.storage' must be 0 or greater")
}

func TestValidatePodmanMetadata_ResourcesBlockAbsent(t *testing.T) {
	// When the resources block is entirely absent all sub-fields default to zero,
	// so cpu and memory checks fire first.
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'resources.cpu' must be greater than 0")
}


// -----------------------------------------------------------------------
// resources key restriction — unknown keys must be rejected
// -----------------------------------------------------------------------

func TestValidatePodmanMetadata_UnknownResourcesKey(t *testing.T) {
	// "gpu" is not one of the four permitted resource keys.
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n  gpu: 2\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusBadRequest, "gpu")
}

func TestValidatePodmanMetadata_UnknownTopLevelKey(t *testing.T) {
	err := bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nunknownField: true\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusBadRequest, "unknownField")
}

func TestValidatePodmanMetadata_AcceleratorsMap(t *testing.T) {
	// accelerators as a map is valid (e.g. IBM Spyre bundles).
	require.NoError(t, bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 8\n  memory: 161061273600\n  storage: 53687091200\n  accelerators:\n    ibm.com/spyre_pf: 4\n"),
		"1.0.0",
	))
}

func TestValidatePodmanMetadata_NoAccelerators(t *testing.T) {
	// accelerators may be omitted entirely (CPU-only bundles).
	require.NoError(t, bundlemetadata.ValidatePodmanMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 2\n  memory: 4294967296\n  storage: 10737418240\n"),
		"1.0.0",
	))
}

func TestParseAndValidateOpenShiftMetadata_UnknownResourcesKey(t *testing.T) {
	_, err := bundlemetadata.ParseAndValidateOpenShiftMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n  disk: 100\n"),
	)
	assertValidationError(t, err, http.StatusBadRequest, "disk")
}

func TestParseAndValidateOpenShiftMetadata_UnknownTopLevelKey(t *testing.T) {
	_, err := bundlemetadata.ParseAndValidateOpenShiftMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nextraKey: value\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n"),
	)
	assertValidationError(t, err, http.StatusBadRequest, "extraKey")
}

func TestParseAndValidateOpenShiftMetadata_AcceleratorsMap(t *testing.T) {
	ver, err := bundlemetadata.ParseAndValidateOpenShiftMetadata(
		[]byte("name: svc\nversion: \"1.0.0\"\nresources:\n  cpu: 12\n  memory: 161061273600\n  storage: 53687091200\n  accelerators:\n    ibm.com/spyre_pf: 4\n"),
	)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", ver)
}
