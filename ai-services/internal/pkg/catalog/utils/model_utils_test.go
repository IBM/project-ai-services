package utils

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestModelsNamespace(t *testing.T) {
	assert.Equal(t, "ai-services-models", ModelsNamespace())
}

func TestModelHelmReleaseName(t *testing.T) {
	id := uuid.MustParse("2b4410e6-1234-5678-9abc-def012345678")

	assert.Equal(t, "llm-2b4410e6", ModelHelmReleaseName(id, "llm"))
	assert.Equal(t, "embedding-2b4410e6", ModelHelmReleaseName(id, "embedding"))
	assert.Equal(t, "reranker-2b4410e6", ModelHelmReleaseName(id, "reranker"))
	assert.Equal(t, "vector-store-2b4410e6", ModelHelmReleaseName(id, "vector_store"))
}

func TestModelHelmReleaseNameRegex(t *testing.T) {
	tests := []struct {
		releaseName string
		wantMatch   bool
	}{
		{"llm-2b4410e6", true},
		{"embedding-c08f9a8b", true},
		{"reranker-a1b2c3d4", true},
		{"vector-store-12345678", true},
		{"custom_model-1a2b3c4d", true},
		{"llm-2b4410e", false},          // only 7 hex chars
		{"llm-2b4410e6a", false},         // 9 hex chars
		{"llm-2b4410eg", false},          // invalid hex char 'g'
		{"ai-services-worker", false},     // not a model release
		{"serving-runtime-cpu", false},    // not a model release
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.releaseName, func(t *testing.T) {
			assert.Equal(t, tt.wantMatch, IsModelHelmReleaseName(tt.releaseName))
			assert.Equal(t, tt.wantMatch, ModelHelmReleaseNameRegex.MatchString(tt.releaseName))
		})
	}
}
