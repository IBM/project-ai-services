package bundlemetadata_test

import (
	"net/http"
	"testing"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
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
// ValidateOpenShiftMetadata — version cross-check
// -----------------------------------------------------------------------

func validOpenShiftMeta(version string) []byte {
	return []byte("name: svc\nversion: " + version + "\nresources:\n  cpu: 1\n  memory: 1073741824\n  storage: 0\n")
}

func TestValidateOpenShiftMetadata_VersionMatch(t *testing.T) {
	require.NoError(t, bundlemetadata.ValidateOpenShiftMetadata(validOpenShiftMeta(`"1.0.0"`), "1.0.0"))
}

func TestValidateOpenShiftMetadata_VersionMismatch(t *testing.T) {
	err := bundlemetadata.ValidateOpenShiftMetadata(validOpenShiftMeta(`"2.0.0"`), "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/metadata.yaml")
}

func TestValidateOpenShiftMetadata_MissingFields(t *testing.T) {
	err := bundlemetadata.ValidateOpenShiftMetadata(
		[]byte("name: svc\nresources:\n  cpu: 1\n  memory: 1073741824\n"),
		"1.0.0",
	)
	assertValidationError(t, err, http.StatusUnprocessableEntity, "'version' is required")
}

// -----------------------------------------------------------------------
// ValidateOpenShiftChartVersion — version cross-check
// -----------------------------------------------------------------------

func TestValidateOpenShiftChartVersion_Match(t *testing.T) {
	chart := []byte("apiVersion: v2\nname: svc\nversion: 1.0.0\ntype: application\n")
	require.NoError(t, bundlemetadata.ValidateOpenShiftChartVersion(chart, "1.0.0"))
}

func TestValidateOpenShiftChartVersion_Mismatch(t *testing.T) {
	chart := []byte("apiVersion: v2\nname: svc\nversion: 2.0.0\ntype: application\n")
	err := bundlemetadata.ValidateOpenShiftChartVersion(chart, "1.0.0")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "version mismatch")
	assertValidationError(t, err, http.StatusUnprocessableEntity, "openshift/Chart.yaml")
}

func TestValidateOpenShiftChartVersion_InvalidYAML(t *testing.T) {
	err := bundlemetadata.ValidateOpenShiftChartVersion([]byte(":\tinvalid"), "1.0.0")
	assertValidationError(t, err, http.StatusBadRequest, "openshift/Chart.yaml")
}
