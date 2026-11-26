package catalog

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	chartcommonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8syaml "sigs.k8s.io/yaml"
)

const (
	tritonPodTemplate   = "components/ml_inference/triton-cpu/podman/templates/triton-server.yaml.tmpl"
	tritonOpenShiftPath = "components/ml_inference/triton-cpu/openshift"
)

// renderTritonPod renders the Triton pod template the way the deployer does and parses it.
func renderTritonPod(t *testing.T, values map[string]any) *models.PodSpec {
	t.Helper()

	raw, err := assets.CatalogFS.ReadFile(tritonPodTemplate)
	require.NoError(t, err)

	tmpl, err := texttemplate.New("triton").Parse(string(raw))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, map[string]any{
		"InstanceSlug": "test1234ab",
		"TemplateID":   "00000000-0000-0000-0000-000000000000",
		"BaseDir":      "/var/lib/ai-services",
		"Values":       values,
		"env":          map[string]map[string]string{},
	}))

	var podSpec models.PodSpec
	require.NoError(t, k8syaml.Unmarshal(buf.Bytes(), &podSpec))

	return &podSpec
}

func tritonValues(memoryLimit string) map[string]any {
	return map[string]any{
		"image":           "quay.io/powercloud/tritonserver:latest",
		"memoryLimit":     memoryLimit,
		"repositoryOwner": "1001:0",
	}
}

func TestTritonPodTemplate(t *testing.T) {
	podSpec := renderTritonPod(t, tritonValues(""))

	require.Len(t, podSpec.Spec.Containers, 1)
	triton := podSpec.Spec.Containers[0]

	assert.Equal(t, "triton-test1234ab", podSpec.Name)

	// Only other pods of the application can reach Triton: no route and no published port.
	assert.NotContains(t, podSpec.Labels, "ai-services.io/routes")
	assert.NotContains(t, podSpec.Labels, "ai-services.io/ports")
	assert.NotContains(t, podSpec.Annotations, "ai-services.io/routes")
	assert.NotContains(t, podSpec.Annotations, "ai-services.io/ports")

	// The deployer hands the first containerPort of the first container to consumers.
	require.NotEmpty(t, triton.Ports)
	assert.EqualValues(t, 8000, triton.Ports[0].ContainerPort)

	assert.Contains(t, triton.Args, "--model-repository=/models")
	assert.Contains(t, triton.Args, "--model-control-mode=explicit")
	assert.Contains(t, triton.Args, "--strict-readiness=false")
	assert.Contains(t, triton.Args, "--exit-on-error=false")
	assert.NotContains(t, triton.Args, "--load-model")

	// The ppc64le Triton build has no gRPC endpoint and rejects the flag, so neither the flag nor
	// the port may be set.
	for _, arg := range triton.Args {
		assert.NotContains(t, arg, "grpc")
	}
	for _, port := range triton.Ports {
		assert.NotEqual(t, int32(8001), port.ContainerPort)
	}

	// The repository volume is cleaned up with the Triton pod only, and Triton mounts it read-only.
	assert.Equal(t, "triton-repo-test1234ab", podSpec.Labels["ai-services.io/volume"])
	require.Len(t, triton.VolumeMounts, 1)
	assert.Equal(t, "/models:z", triton.VolumeMounts[0].MountPath)
	assert.True(t, triton.VolumeMounts[0].ReadOnly)

	require.Len(t, podSpec.Spec.Volumes, 1)
	require.NotNil(t, podSpec.Spec.Volumes[0].PersistentVolumeClaim)
	assert.Equal(t, "triton-repo-test1234ab", podSpec.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)

	// No memory limit is set until DEV provides it.
	assert.Empty(t, triton.Resources.Limits)
}

func TestTritonPodTemplate_RepositoryPermissions(t *testing.T) {
	podSpec := renderTritonPod(t, tritonValues(""))

	require.Len(t, podSpec.Spec.InitContainers, 1)
	initContainer := podSpec.Spec.InitContainers[0]

	// The init container hands the volume root to the Forecast user and keeps it world-readable.
	// It also creates .staging: Podman resets the owner of an empty volume when Triton starts.
	require.NotNil(t, initContainer.SecurityContext)
	require.NotNil(t, initContainer.SecurityContext.RunAsUser)
	assert.EqualValues(t, 0, *initContainer.SecurityContext.RunAsUser)
	assert.Equal(t, []string{
		"sh", "-c",
		"mkdir -p /models/.staging && chown 1001:0 /models /models/.staging && chmod 0755 /models /models/.staging",
	}, initContainer.Command)

	require.Len(t, initContainer.VolumeMounts, 1)
	assert.Equal(t, "/models:z", initContainer.VolumeMounts[0].MountPath)
	assert.False(t, initContainer.VolumeMounts[0].ReadOnly)
}

func TestTritonPodTemplate_MemoryLimit(t *testing.T) {
	podSpec := renderTritonPod(t, tritonValues("8Gi"))

	triton := podSpec.Spec.Containers[0]
	mem := triton.Resources.Limits["memory"]
	assert.Equal(t, "8Gi", mem.String())
	mem = triton.Resources.Requests["memory"]
	assert.Equal(t, "8Gi", mem.String())
}

func TestLoadComponent_TritonCPU(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	require.NoError(t, err)

	component, err := provider.LoadComponent("ml_inference", "triton-cpu")
	require.NoError(t, err)
	assert.Equal(t, "triton-cpu", component.ID)
	assert.Equal(t, "ml_inference", component.ComponentType.ID)
}

// renderTritonChart renders the OpenShift chart with the chart defaults plus overrides.
func renderTritonChart(t *testing.T, overrides map[string]any) map[string]map[string]any {
	t.Helper()

	var files []*archive.BufferedFile
	require.NoError(t, fs.WalkDir(&assets.CatalogFS, tritonOpenShiftPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(&assets.CatalogFS, p)
		if err != nil {
			return err
		}
		files = append(files, &archive.BufferedFile{Name: strings.TrimPrefix(p, tritonOpenShiftPath+"/"), Data: data})

		return nil
	}))

	c, err := loader.LoadFiles(files)
	require.NoError(t, err)
	require.NoError(t, c.Validate())

	vals := map[string]any{"templateID": "00000000-0000-0000-0000-000000000000"}
	for k, v := range overrides {
		vals[k] = v
	}

	renderVals, err := chartcommonutil.ToRenderValuesWithSchemaValidation(c, vals, common.ReleaseOptions{
		Name:      "triton-test",
		Namespace: "default",
	}, nil, true)
	require.NoError(t, err)

	rendered, err := engine.Render(c, renderVals)
	require.NoError(t, err)

	docs := make(map[string]map[string]any)
	for file, content := range rendered {
		var doc map[string]any
		require.NoError(t, k8syaml.Unmarshal([]byte(content), &doc), file)
		if doc != nil {
			docs[filepath.Base(file)] = doc
		}
	}

	return docs
}

func TestTritonOpenShiftChart(t *testing.T) {
	docs := renderTritonChart(t, nil)
	require.Len(t, docs, 3)

	// PersistentVolumeClaim: ReadWriteMany and kept when the chart is removed.
	pvc := docs["triton-repository-pvc.yaml"]
	require.NotNil(t, pvc)
	assert.Equal(t, "PersistentVolumeClaim", pvc["kind"])
	pvcMeta := pvc["metadata"].(map[string]any)
	assert.Equal(t, "triton-repository", pvcMeta["name"])
	assert.Equal(t, "keep", pvcMeta["annotations"].(map[string]any)["helm.sh/resource-policy"])
	assert.Equal(t, []any{"ReadWriteMany"}, pvc["spec"].(map[string]any)["accessModes"])

	// Deployment: Recreate, read-only repository mount, readiness probe on /v2/health/ready.
	var dep appsv1.Deployment
	raw, err := k8syaml.Marshal(docs["triton-deployment.yaml"])
	require.NoError(t, err)
	require.NoError(t, k8syaml.Unmarshal(raw, &dep))

	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, dep.Spec.Strategy.Type)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	triton := dep.Spec.Template.Spec.Containers[0]

	assert.Contains(t, triton.Args, "--model-control-mode=explicit")
	assert.Contains(t, triton.Args, "--exit-on-error=false")
	assert.Contains(t, triton.Args, "--strict-readiness=false")
	require.NotNil(t, triton.ReadinessProbe)
	assert.Equal(t, "/v2/health/ready", triton.ReadinessProbe.HTTPGet.Path)
	require.NotNil(t, triton.LivenessProbe)
	assert.Equal(t, "/v2/health/live", triton.LivenessProbe.HTTPGet.Path)
	assert.EqualValues(t, 8000, triton.Ports[0].ContainerPort)
	for _, arg := range triton.Args {
		assert.NotContains(t, arg, "grpc", "this Triton build has no gRPC endpoint")
	}
	for _, port := range triton.Ports {
		assert.NotEqual(t, int32(8001), port.ContainerPort)
	}

	require.Len(t, triton.VolumeMounts, 1)
	assert.Equal(t, "/models", triton.VolumeMounts[0].MountPath)
	assert.True(t, triton.VolumeMounts[0].ReadOnly)
	assert.Equal(t, "triton-repository", dep.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	assert.Empty(t, triton.Resources.Limits, "no limits until DEV provides them")

	// Service: ClusterIP on 8000, and no Route is rendered.
	svc := docs["triton-service.yaml"]
	require.NotNil(t, svc)
	assert.Equal(t, "triton", svc["metadata"].(map[string]any)["name"])
	assert.Equal(t, "ClusterIP", svc["spec"].(map[string]any)["type"])
	assert.EqualValues(t, 8000, svc["spec"].(map[string]any)["ports"].([]any)[0].(map[string]any)["port"])
	for _, port := range svc["spec"].(map[string]any)["ports"].([]any) {
		assert.NotEqual(t, "grpc", port.(map[string]any)["name"])
	}
}

func TestTritonOpenShiftChart_Resources(t *testing.T) {
	docs := renderTritonChart(t, map[string]any{
		"storageClassName": "ibm-spectrum-scale",
		"storage":          "20Gi",
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "2", "memory": "4Gi"},
			"limits":   map[string]any{"memory": "8Gi"},
		},
	})

	spec := docs["triton-repository-pvc.yaml"]["spec"].(map[string]any)
	assert.Equal(t, "ibm-spectrum-scale", spec["storageClassName"])
	assert.Equal(t, "20Gi", spec["resources"].(map[string]any)["requests"].(map[string]any)["storage"])

	var dep appsv1.Deployment
	raw, err := k8syaml.Marshal(docs["triton-deployment.yaml"])
	require.NoError(t, err)
	require.NoError(t, k8syaml.Unmarshal(raw, &dep))

	res := dep.Spec.Template.Spec.Containers[0].Resources
	cpuReq, memReq, memLim := res.Requests["cpu"], res.Requests["memory"], res.Limits["memory"]
	assert.Equal(t, "2", cpuReq.String())
	assert.Equal(t, "4Gi", memReq.String())
	assert.Equal(t, "8Gi", memLim.String())
	assert.NotContains(t, res.Limits, corev1.ResourceCPU)
}
