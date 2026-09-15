package podman

import (
	"bytes"
	"testing"
	"text/template"
	"time"

	v1 "github.com/containers/podman/v5/pkg/k8s.io/api/core/v1"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8syaml "sigs.k8s.io/yaml"
)

// loadLLMPodSpec renders a vllm-server.yaml.tmpl from the embedded CatalogFS
// with the minimal template params used in production, then unmarshals it into
// a models.PodSpec.  This exercises the exact same code path as the deployer.
func loadLLMPodSpec(t *testing.T, templatePath string) *models.PodSpec {
	t.Helper()

	raw, err := assets.CatalogFS.ReadFile(templatePath)
	require.NoError(t, err, "reading template file %s", templatePath)

	tmpl, err := template.New("vllm-server").Parse(string(raw))
	require.NoError(t, err, "parsing template %s", templatePath)

	// Minimal params — mirror the fields used in production rendering.
	params := map[string]any{
		"InstanceSlug": "test1234ab",
		"TemplateID":   "00000000-0000-0000-0000-000000000000",
		"BaseDir":      "/var/lib/ai-services",
		"Values": map[string]any{
			"image":               "icr.io/ppc64le-oss/vllm-ppc64le:0.28.0",
			"model":               "ibm-granite/granite-3.3-8b-instruct",
			"apiKey":              "",
			"maxNumBatchedTokens": "8096",
			"maxModelLen":         "8096",
			"maxBatchSize":        "32",
		},
		"env": map[string]map[string]string{},
	}

	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, params), "rendering template %s", templatePath)

	var podSpec models.PodSpec
	require.NoError(t, k8syaml.Unmarshal(buf.Bytes(), &podSpec), "unmarshalling pod spec from %s", templatePath)

	return &podSpec
}

// ---- vllm-cpu template tests ------------------------------------------------

func TestFetchLivenessProbe_VllmCPU(t *testing.T) {
	podSpec := loadLLMPodSpec(t, "components/llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl")

	// The container name in the template is "llm".
	probe := fetchLivenessProbe(podSpec, "llm")
	require.NotNil(t, probe, "expected a liveness probe on the 'llm' container")

	// Verify each field the spec declares.
	assert.EqualValues(t, 120, probe.InitialDelaySeconds, "initialDelaySeconds")
	assert.EqualValues(t, 30, probe.PeriodSeconds, "periodSeconds")
	assert.EqualValues(t, 5, probe.TimeoutSeconds, "timeoutSeconds")
	assert.EqualValues(t, 12, probe.FailureThreshold, "failureThreshold")
	require.NotNil(t, probe.HTTPGet, "expected httpGet probe")
	assert.Equal(t, "/health", probe.HTTPGet.Path)
	assert.EqualValues(t, 8000, probe.HTTPGet.Port.IntValue())
}

func TestFetchLivenessProbe_InfraContainer_ReturnsNil(t *testing.T) {
	podSpec := loadLLMPodSpec(t, "components/llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl")

	// The infra container is injected by Podman at runtime; it is absent from the spec.
	probe := fetchLivenessProbe(podSpec, "test1234ab-infra")
	assert.Nil(t, probe, "infra container should have no probe in the pod spec")
}

func TestComputeReadinessTimeout_VllmCPU(t *testing.T) {
	podSpec := loadLLMPodSpec(t, "components/llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl")

	probe := fetchLivenessProbe(podSpec, "llm")
	require.NotNil(t, probe)

	timeout, pollInterval := computeReadinessTimeout(probe)

	// Expected:
	//   initialDelaySeconds = 120 s
	//   failureThreshold    = 12
	//   periodSeconds       = 30 s
	//   buffer              = 30 s  (readinessTimeoutBuffer)
	//
	//   timeout      = 120 + (12 × 30) + 30 = 510 s = 8m30s
	//   pollInterval = 30 s
	assert.Equal(t, 510*time.Second, timeout, "readiness timeout for vllm-cpu")
	assert.Equal(t, 30*time.Second, pollInterval, "poll interval for vllm-cpu")
}

// ---- vllm-spyre template tests ----------------------------------------------

func TestFetchLivenessProbe_VllmSpyre(t *testing.T) {
	podSpec := loadLLMPodSpec(t, "components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl")

	probe := fetchLivenessProbe(podSpec, "llm")
	require.NotNil(t, probe, "expected a liveness probe on the 'llm' container")

	assert.EqualValues(t, 420, probe.InitialDelaySeconds, "initialDelaySeconds")
	assert.EqualValues(t, 30, probe.PeriodSeconds, "periodSeconds")
	assert.EqualValues(t, 5, probe.TimeoutSeconds, "timeoutSeconds")
	assert.EqualValues(t, 3, probe.FailureThreshold, "failureThreshold")
	require.NotNil(t, probe.HTTPGet, "expected httpGet probe")
	assert.Equal(t, "/health", probe.HTTPGet.Path)
	assert.EqualValues(t, 8000, probe.HTTPGet.Port.IntValue())
}

func TestComputeReadinessTimeout_VllmSpyre(t *testing.T) {
	podSpec := loadLLMPodSpec(t, "components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl")

	probe := fetchLivenessProbe(podSpec, "llm")
	require.NotNil(t, probe)

	timeout, pollInterval := computeReadinessTimeout(probe)

	// Expected:
	//   initialDelaySeconds = 420 s
	//   failureThreshold    = 3
	//   periodSeconds       = 30 s
	//   buffer              = 30 s
	//
	//   timeout      = 420 + (3 × 30) + 30 = 540 s = 9m
	//   pollInterval = 30 s
	assert.Equal(t, 540*time.Second, timeout, "readiness timeout for vllm-spyre")
	assert.Equal(t, 30*time.Second, pollInterval, "poll interval for vllm-spyre")
}

// ---- computeReadinessTimeout edge-case tests --------------------------------

func TestComputeReadinessTimeout_UsesK8sDefaultsWhenFieldsZero(t *testing.T) {
	// A probe with only initialDelaySeconds set; period and failure threshold
	// should fall back to the Kubernetes defaults (10 s and 3 respectively).
	probe := &v1.Probe{
		InitialDelaySeconds: 60,
		// PeriodSeconds and FailureThreshold left at zero → use defaults
	}

	timeout, pollInterval := computeReadinessTimeout(probe)

	// timeout = 60 + (3 × 10) + 30 = 120 s
	assert.Equal(t, 120*time.Second, timeout)
	assert.Equal(t, 10*time.Second, pollInterval, "should fall back to defaultProbePeriodSeconds")
}

func TestComputeReadinessTimeout_NoInitialDelay(t *testing.T) {
	probe := &v1.Probe{
		PeriodSeconds:    30,
		FailureThreshold: 3,
	}

	timeout, pollInterval := computeReadinessTimeout(probe)

	// timeout = 0 + (3 × 30) + 30 = 120 s
	assert.Equal(t, 120*time.Second, timeout)
	assert.Equal(t, 30*time.Second, pollInterval)
}

func TestFetchLivenessProbe_NilPodSpec(t *testing.T) {
	probe := fetchLivenessProbe(nil, "llm")
	assert.Nil(t, probe, "nil podSpec should return nil probe")
}
