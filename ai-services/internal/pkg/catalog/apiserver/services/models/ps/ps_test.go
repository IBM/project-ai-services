package ps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWorkerResolver struct {
	rt      runtime.Runtime
	err     error
	rtType  string
	name    string
	idFound bool
}

func (f *fakeWorkerResolver) ID(_ string) (uuid.UUID, bool) {
	return uuid.New(), f.idFound
}

func (f *fakeWorkerResolver) Info(_ context.Context, _ uuid.UUID) (string, string, error) {
	return f.name, f.rtType, f.err
}

func (f *fakeWorkerResolver) Validate(_ context.Context, _ string) error {
	return f.err
}

func (f *fakeWorkerResolver) RuntimeType(_ context.Context, _ string) (string, error) {
	return f.rtType, f.err
}

func (f *fakeWorkerResolver) DomainSuffix(_ string) (string, error) {
	return ".nip.io", f.err
}

func (f *fakeWorkerResolver) PrivateIngressPort(_ string) string {
	return "8080"
}

func (f *fakeWorkerResolver) NewRuntime(_ context.Context, _, _ string) (runtime.Runtime, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.rt, nil
}

func TestGetPS_RemoteModel(t *testing.T) {
	modelID := uuid.New()
	row := &dbrepo.ModelRow{
		ID:             modelID,
		Name:           "watsonx-llm",
		DeploymentType: catalogconstants.DeploymentTypeRemote,
		ComponentType:  "llm",
		Provider:       "watsonx",
		Model:          "ibm/granite-4-h-small",
	}

	getter := New(&fakeWorkerResolver{})
	resp, err := getter.GetPS(context.Background(), row)

	require.NoError(t, err)
	assert.Equal(t, modelID.String(), resp.ID)
	assert.Equal(t, "watsonx-llm", resp.Name)
	assert.Equal(t, "remote", resp.DeploymentType)
	assert.Equal(t, "llm", resp.ComponentType)
	assert.Equal(t, "watsonx", resp.ProviderID)
	assert.Equal(t, "ibm/granite-4-h-small", resp.Model)
	assert.Empty(t, resp.Pods)
}

func TestGetPS_LocalModel_Podman(t *testing.T) {
	modelID := uuid.New()
	mockRT := mock.New(runtimeTypes.RuntimeTypePodman)

	// Create a mock pod labelled with the component ID
	podManifest := `
apiVersion: v1
kind: Pod
metadata:
  name: llm-pod
  labels:
    ` + constants.ApplicationTemplateKey + `: ` + modelID.String() + `
spec:
  containers:
  - name: vllm
    image: vllm:latest
`
	_, err := mockRT.CreatePod(context.Background(), strings.NewReader(podManifest), nil)
	require.NoError(t, err)

	row := &dbrepo.ModelRow{
		ID:                modelID,
		Name:              "granite-local",
		DeploymentType:    catalogconstants.DeploymentTypeLocal,
		ComponentType:     "llm",
		Provider:          "vllm-cpu",
		Model:             "ibm-granite/granite-3.3-8b-instruct",
		WorkerName:        "Local",
		WorkerRuntimeType: "podman",
	}

	resolver := &fakeWorkerResolver{rt: mockRT, rtType: "podman"}
	getter := New(resolver)
	resp, err := getter.GetPS(context.Background(), row)

	require.NoError(t, err)
	assert.Equal(t, modelID.String(), resp.ID)
	assert.Equal(t, "granite-local", resp.Name)
	assert.Equal(t, "local", resp.DeploymentType)
	assert.Equal(t, "llm", resp.ComponentType)
	assert.Equal(t, "vllm-cpu", resp.ProviderID)
	assert.Equal(t, "Local", resp.WorkerName)
	assert.Equal(t, "podman", resp.RuntimeType)
	assert.Len(t, resp.Pods, 1)
	assert.Equal(t, "llm-pod", resp.Pods[0].PodName)
	assert.True(t, resp.Pods[0].Healthy)
	assert.Equal(t, "running", string(resp.Pods[0].Status))
}

func TestGetPS_WorkerUnreachable(t *testing.T) {
	modelID := uuid.New()
	row := &dbrepo.ModelRow{
		ID:                modelID,
		Name:              "granite-spyre",
		DeploymentType:    catalogconstants.DeploymentTypeLocal,
		ComponentType:     "llm",
		Provider:          "vllm-spyre",
		WorkerName:        "lpar-1",
		WorkerRuntimeType: "podman",
	}

	resolver := &fakeWorkerResolver{err: errors.New("connection refused")}
	getter := New(resolver)
	_, err := getter.GetPS(context.Background(), row)

	require.Error(t, err)
	var valErr *validators.ValidationError
	require.True(t, errors.As(err, &valErr))
	assert.Equal(t, 422, valErr.Code)
}

func TestGetPS_LocalModel_OpenShiftNamespace(t *testing.T) {
	modelID := uuid.New()
	mockRT := mock.New(runtimeTypes.RuntimeTypeOpenShift)

	row := &dbrepo.ModelRow{
		ID:                modelID,
		Name:              "granite-oc",
		DeploymentType:    catalogconstants.DeploymentTypeLocal,
		ComponentType:     "llm",
		Provider:          "vllm-cpu",
		WorkerName:        "ocp-1",
		WorkerRuntimeType: "openshift",
	}

	resolver := &fakeWorkerResolver{rt: mockRT, rtType: "openshift"}
	getter := New(resolver)
	resp, err := getter.GetPS(context.Background(), row)

	require.NoError(t, err)
	assert.Equal(t, "ai-services-models", resp.Namespace)
	assert.Empty(t, resp.Pods)
}
