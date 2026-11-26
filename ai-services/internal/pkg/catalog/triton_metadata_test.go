package catalog_test

import (
	"testing"

	"github.com/project-ai-services/ai-services/assets"
	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/stretchr/testify/require"
)

// The Catalog's own bundle validator requires cpu and memory above 0, so placeholders of 0 would
// make the Triton component fail validation. The validator package imports the catalog package,
// so this test lives in the external test package.
func TestTritonRuntimeMetadataIsValid(t *testing.T) {
	const base = "components/ml_inference/triton-cpu/"

	podman, err := assets.CatalogFS.ReadFile(base + "podman/metadata.yaml")
	require.NoError(t, err)
	require.NoError(t, bundlemetadata.ValidatePodmanMetadata(podman, "1.0.0"))

	openshift, err := assets.CatalogFS.ReadFile(base + "openshift/metadata.yaml")
	require.NoError(t, err)
	_, err = bundlemetadata.ParseAndValidateOpenShiftMetadata(openshift)
	require.NoError(t, err)
}
