package openshift

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	helmutil "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPredictorAddress(t *testing.T) {
	assert.Equal(t, "llm-predictor.ai-services-model-1234abcd.svc.cluster.local:8080",
		PredictorAddress("llm", "ai-services-model-1234abcd"))
}

func TestNewHelmManager_LocalRuntime(t *testing.T) {
	got := NewHelmManager(mock.New(runtimeTypes.RuntimeTypeOpenShift), "ns")

	assert.IsType(t, &helmutil.LocalHelmManager{}, got, "a runtime that is not remote uses the local kubeconfig")
}

func TestWaitForInferenceServiceReady_SkipsChartWithoutInferenceService(t *testing.T) {
	fsys := fstest.MapFS{
		"chart/templates/secret.yaml": {Data: []byte("kind: Secret\n")},
	}

	// A failing wait would be reported, so a nil error proves the wait was skipped.
	rt := &failingWaitRuntime{Runtime: mock.New(runtimeTypes.RuntimeTypeOpenShift)}

	require.NoError(t, WaitForInferenceServiceReady(context.Background(), rt, fsys, "chart", "llm"))
	assert.False(t, rt.waited)
}

func TestWaitForInferenceServiceReady_WaitsForInferenceService(t *testing.T) {
	fsys := fstest.MapFS{
		"chart/templates/isvc.yaml": {Data: []byte("kind: InferenceService\n")},
	}

	t.Run("ready", func(t *testing.T) {
		rt := &failingWaitRuntime{Runtime: mock.New(runtimeTypes.RuntimeTypeOpenShift)}

		require.NoError(t, WaitForInferenceServiceReady(context.Background(), rt, fsys, "chart", "llm"))
		assert.True(t, rt.waited)
		assert.Equal(t, "llm", rt.isvcName)
	})

	t.Run("not ready", func(t *testing.T) {
		rt := &failingWaitRuntime{Runtime: mock.New(runtimeTypes.RuntimeTypeOpenShift), err: errors.New("timed out")}

		err := WaitForInferenceServiceReady(context.Background(), rt, fsys, "chart", "llm")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `InferenceService "llm" is not ready yet`)
	})
}

func TestUninstallServingRuntimes(t *testing.T) {
	t.Run("nothing to uninstall", func(t *testing.T) {
		rt := mock.New(runtimeTypes.RuntimeTypeOpenShift)

		assert.Empty(t, UninstallServingRuntimes(context.Background(), rt, &fakeHelm{}))
	})

	t.Run("listing the serving runtimes fails", func(t *testing.T) {
		rt := &failingWaitRuntime{Runtime: mock.New(runtimeTypes.RuntimeTypeOpenShift), listErr: errors.New("api down")}

		errs := UninstallServingRuntimes(context.Background(), rt, &fakeHelm{})

		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "failed to list serving runtimes: api down")
	})

	t.Run("each release is uninstalled and every failure is reported", func(t *testing.T) {
		rt := &failingWaitRuntime{
			Runtime: mock.New(runtimeTypes.RuntimeTypeOpenShift),
			crds: []runtimeTypes.CRDResource{
				{Name: "a", Labels: map[string]string{"ai-services.io/prerequisite": "serving-runtime-cpu"}},
				{Name: "serving-runtime-spyre"}, // no label value: falls back to the resource name
				{Name: "c", Labels: map[string]string{"ai-services.io/prerequisite": "broken"}},
			},
		}
		helm := &fakeHelm{failOn: "broken"}

		errs := UninstallServingRuntimes(context.Background(), rt, helm)

		assert.Equal(t, []string{"serving-runtime-cpu", "serving-runtime-spyre", "broken"}, helm.uninstalled)
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "serving runtime 'broken': helm uninstall failed")
	})
}

// failingWaitRuntime wraps the mock runtime to record and fail the calls the helpers make.
type failingWaitRuntime struct {
	runtime.Runtime
	err      error
	waited   bool
	isvcName string
	listErr  error
	crds     []runtimeTypes.CRDResource
}

func (f *failingWaitRuntime) WaitForInferenceServiceReady(_ context.Context, isvcName string) error {
	f.waited = true
	f.isvcName = isvcName

	return f.err
}

func (f *failingWaitRuntime) ListCRD(context.Context, *unstructured.UnstructuredList, map[string][]string) ([]runtimeTypes.CRDResource, error) {
	return f.crds, f.listErr
}

// fakeHelm records the releases it was asked to uninstall.
type fakeHelm struct {
	helmutil.HelmManager
	uninstalled []string
	failOn      string
}

func (f *fakeHelm) Uninstall(_ context.Context, release string) error {
	f.uninstalled = append(f.uninstalled, release)

	if release == f.failOn {
		return errors.New("uninstall failed")
	}

	return nil
}
