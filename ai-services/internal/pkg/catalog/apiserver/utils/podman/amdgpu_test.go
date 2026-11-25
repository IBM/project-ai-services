package podman

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "go.podman.io/podman/v6/pkg/k8s.io/api/core/v1"
	metav1 "go.podman.io/podman/v6/pkg/k8s.io/apimachinery/pkg/apis/meta/v1"

	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
)

func TestFetchAMDGPUsFromPodAnnotations(t *testing.T) {
	total, perContainer, err := FetchAMDGPUsFromPodAnnotations(map[string]string{
		"ai-services.io/llm--amd-gpus":    "2",
		"ai-services.io/llm--spyre-cards": "4",
		"some.other/annotation":           "x",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Equal(t, map[string]int{"llm": 2}, perContainer)

	_, _, err = FetchAMDGPUsFromPodAnnotations(map[string]string{"ai-services.io/llm--amd-gpus": "abc"})
	require.Error(t, err)
}

func TestGetEnvParams_AMDGPUs(t *testing.T) {
	podSpec := newPodSpec(map[string]string{"ai-services.io/llm--amd-gpus": "2"})

	t.Run("injects ROCR_VISIBLE_DEVICES from the pool", func(t *testing.T) {
		pool := &modeltypes.AMDGPUPool{Addresses: []string{"0", "1", "3"}}

		env, err := GetEnvParams(context.Background(), podSpec, nil, pool)
		require.NoError(t, err)
		assert.Equal(t, "0,1", env["llm"]["ROCR_VISIBLE_DEVICES"])
		assert.Equal(t, []string{"3"}, pool.Addresses, "allocated ordinals are removed from the pool")
	})

	t.Run("errors when the pool was not populated", func(t *testing.T) {
		_, err := GetEnvParams(context.Background(), podSpec, nil, nil)
		require.Error(t, err)
	})

	t.Run("errors when the pool has too few GPUs", func(t *testing.T) {
		_, err := GetEnvParams(context.Background(), podSpec, nil, &modeltypes.AMDGPUPool{Addresses: []string{"0"}})
		require.Error(t, err)
	})

	t.Run("no env when no GPUs are requested", func(t *testing.T) {
		env, err := GetEnvParams(context.Background(), newPodSpec(nil), nil, nil)
		require.NoError(t, err)
		assert.Empty(t, env["llm"])
	})
}

func newPodSpec(annotations map[string]string) *podmodels.PodSpec {
	return &podmodels.PodSpec{Pod: v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "llm-pod", Annotations: annotations},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "llm"}}},
	}}
}
