package credentialspec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/credentialspec"
)

func TestParse_NoMarkerYieldsNothing(t *testing.T) {
	specs, err := credentialspec.Parse([]byte("a: 1\nb: \"\"\n"))

	require.NoError(t, err)
	assert.Empty(t, specs)
	assert.False(t, credentialspec.Has([]byte("a: 1\n")))
}

func TestParse_ValidAnnotation(t *testing.T) {
	data := `ui:
  port: ""
# Outbound credential, set by the catalog.
# @credential:service grants=similarity:read
authServiceCredentialSecret: ""
other: x
`
	specs, err := credentialspec.Parse([]byte(data))

	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "authServiceCredentialSecret", specs[0].ValuesKey)
	assert.Equal(t, map[string][]string{"similarity": {"read"}}, specs[0].Grants)
}

func TestParse_MultipleGrantsAndSpacesAndDuplicates(t *testing.T) {
	data := "a: 1\n# @credential:service grants=similarity:read, similarity:write, summarize:read, similarity:read\nk: \"\"\n"

	specs, err := credentialspec.Parse([]byte(data))

	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, map[string][]string{
		"similarity": {"read", "write"},
		"summarize":  {"read"},
	}, specs[0].Grants)
}

func TestParse_RejectsEverythingItDoesNotRecognise(t *testing.T) {
	tests := map[string]string{
		"admin role":           "a: 1\n# @credential:service grants=similarity:admin\nk: \"\"\n",
		"unknown role":         "a: 1\n# @credential:service grants=similarity:root\nk: \"\"\n",
		"unknown kind":         "a: 1\n# @credential:user grants=similarity:read\nk: \"\"\n",
		"no grants option":     "a: 1\n# @credential:service\nk: \"\"\n",
		"other option":         "a: 1\n# @credential:service roles=read\nk: \"\"\n",
		"missing role":         "a: 1\n# @credential:service grants=similarity\nk: \"\"\n",
		"empty audience":       "a: 1\n# @credential:service grants=:read\nk: \"\"\n",
		"extra colon":          "a: 1\n# @credential:service grants=similarity:read:x\nk: \"\"\n",
		"empty grant entry":    "a: 1\n# @credential:service grants=similarity:read,\nk: \"\"\n",
		"non-empty value":      "a: 1\n# @credential:service grants=similarity:read\nk: \"preset\"\n",
		"non-scalar value":     "a: 1\n# @credential:service grants=similarity:read\nk:\n  x: 1\n",
		"two on one key":       "a: 1\n# @credential:service grants=similarity:read\n# @credential:service grants=summarize:read\nk: \"\"\n",
		"nested key":           "a:\n  # @credential:service grants=similarity:read\n  k: \"\"\n",
		"detached by blank":    "# @credential:service grants=similarity:read\n\nk: \"\"\n",
		"marker in prose":      "a: 1\n# the @credential: annotation is not used here\nk: \"\"\n",
		"trailing comment":     "a: 1\nk: \"\" # @credential:service grants=similarity:read\n",
		"marker not at line":   "a: 1\n# see @credential:service grants=similarity:read\nk: \"\"\n",
		"not a mapping":        "# @credential:service grants=similarity:read\n- a\n",
		"invalid yaml":         "a: [\n# @credential:service grants=similarity:read\n",
		"marker on empty file": "# @credential:service grants=similarity:read\n",
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			specs, err := credentialspec.Parse([]byte(data))

			require.Error(t, err)
			assert.Empty(t, specs)
		})
	}
}

func TestParse_AnnotationOnFirstKeyOfFile(t *testing.T) {
	specs, err := credentialspec.Parse([]byte("# @credential:service grants=similarity:read\nk: \"\"\n"))

	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "k", specs[0].ValuesKey)
}

func TestHas(t *testing.T) {
	assert.True(t, credentialspec.Has([]byte("k: 1 # @credential:service")))
	assert.False(t, credentialspec.Has([]byte("k: 1 # credential")))
}
