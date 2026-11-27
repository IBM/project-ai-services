package utils

import (
	"context"
	"strings"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func podManifest(name, resourceID string) string {
	return `
apiVersion: v1
kind: Pod
metadata:
  name: ` + name + `
  labels:
    ` + constants.ApplicationTemplateKey + `: ` + resourceID + `
spec:
  containers:
  - name: main
    image: img:latest
`
}

func TestLoadPods_NoPods(t *testing.T) {
	rt := mock.New(runtimeTypes.RuntimeTypePodman)

	pods, err := LoadPods(context.Background(), rt, "missing-id")

	require.NoError(t, err)
	assert.NotNil(t, pods)
	assert.Empty(t, pods)
}

func TestLoadPods_RunningPod(t *testing.T) {
	rt := mock.New(runtimeTypes.RuntimeTypePodman)
	_, err := rt.CreatePod(context.Background(), strings.NewReader(podManifest("llm-pod", "res-1")), nil)
	require.NoError(t, err)

	pods, err := LoadPods(context.Background(), rt, "res-1")

	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "llm-pod", pods[0].PodName)
	assert.Equal(t, "running", string(pods[0].Status))
	assert.True(t, pods[0].Healthy)
	assert.Equal(t, "res-1", pods[0].Labels[constants.ApplicationTemplateKey])
	assert.NotEmpty(t, pods[0].Created)
	assert.NotEmpty(t, pods[0].Containers)
}

func TestLoadPods_OnlyMatchingResource(t *testing.T) {
	rt := mock.New(runtimeTypes.RuntimeTypePodman)
	_, err := rt.CreatePod(context.Background(), strings.NewReader(podManifest("pod-a", "res-a")), nil)
	require.NoError(t, err)
	_, err = rt.CreatePod(context.Background(), strings.NewReader(podManifest("pod-b", "res-b")), nil)
	require.NoError(t, err)

	pods, err := LoadPods(context.Background(), rt, "res-a")

	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "pod-a", pods[0].PodName)
}

// TestLoadPods_ContainerStatusIsItsOwn guards against copying the pod status onto containers. A
// stopped mock pod is "exited" while its containers are "created", so the two must differ.
func TestLoadPods_ContainerStatusIsItsOwn(t *testing.T) {
	rt := mock.New(runtimeTypes.RuntimeTypePodman)
	created, err := rt.CreatePod(context.Background(), strings.NewReader(podManifest("stopped-pod", "res-1")), nil)
	require.NoError(t, err)
	require.NoError(t, rt.StopPod(context.Background(), created[0].ID))

	pods, err := LoadPods(context.Background(), rt, "res-1")

	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "exited", string(pods[0].Status))
	assert.False(t, pods[0].Healthy)
	require.NotEmpty(t, pods[0].Containers)

	for _, c := range pods[0].Containers {
		assert.Equal(t, "created", string(c.Status), "container %s must report its own status, not the pod's", c.Name)
		assert.False(t, c.Healthy)
	}
}
