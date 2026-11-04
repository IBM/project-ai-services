package mock

import (
	"context"
	"strings"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

const kubeManifest = `
apiVersion: v1
kind: Secret
metadata:
  name: db-secret
  labels:
    app: demo
---
apiVersion: v1
kind: Pod
metadata:
  name: demo--postgres
  labels:
    ai-services.io/application: demo
spec:
  containers:
    - name: postgres
      image: postgres:16
      env:
        - name: POSTGRES_DB
          value: postgres
      ports:
        - containerPort: 5432
          hostPort: 15432
      livenessProbe:
        exec:
          command: ["true"]
    - name: sidecar
      image: busybox
`

func TestCreatePodParsesPodsAndSecrets(t *testing.T) {
	ctx := context.Background()
	c := New(types.RuntimeTypePodman)

	pods, err := c.CreatePod(ctx, strings.NewReader(kubeManifest), nil)
	if err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	if len(pods) != 1 || pods[0].Name != "demo--postgres" {
		t.Fatalf("unexpected pods: %+v", pods)
	}

	// Readiness checks expect spec containers + 1 infra container.
	pod, err := c.InspectPod(ctx, pods[0].ID)
	if err != nil {
		t.Fatalf("InspectPod: %v", err)
	}

	if got, want := len(pod.Containers), 3; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}

	if pod.State != "Running" || pod.Ports["5432/tcp"][0] != "15432" {
		t.Fatalf("unexpected pod state/ports: %+v", pod)
	}

	// A container with a liveness probe is healthy; one without reports no health.
	pg, err := c.InspectContainer(ctx, "demo--postgres-postgres")
	if err != nil {
		t.Fatalf("InspectContainer: %v", err)
	}

	if pg.Health != string(constants.Ready) || pg.Env["POSTGRES_DB"] != "postgres" {
		t.Fatalf("unexpected container: %+v", pg)
	}

	if ok, _ := c.SecretExists(ctx, "db-secret"); !ok {
		t.Fatal("secret from manifest not recorded")
	}
}

func TestListPodsFiltersByLabel(t *testing.T) {
	ctx := context.Background()
	c := New(types.RuntimeTypePodman)

	if _, err := c.CreatePod(ctx, strings.NewReader(kubeManifest), nil); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	hit, _ := c.ListPods(ctx, map[string][]string{"label": {"ai-services.io/application=demo"}})
	miss, _ := c.ListPods(ctx, map[string][]string{"label": {"ai-services.io/application=other"}})

	if len(hit) != 1 || len(miss) != 0 {
		t.Fatalf("hit=%d miss=%d, want 1/0", len(hit), len(miss))
	}
}

func TestPodLifecycle(t *testing.T) {
	ctx := context.Background()
	c := New(types.RuntimeTypePodman)

	if _, err := c.CreatePod(ctx, strings.NewReader(kubeManifest), map[string]string{"start": constants.PodStartOff}); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	if p, _ := c.InspectPod(ctx, "demo--postgres"); p.State != "Exited" {
		t.Fatalf("start=off pod state = %q, want Exited", p.State)
	}

	if err := c.StartPod(ctx, "demo--postgres"); err != nil {
		t.Fatalf("StartPod: %v", err)
	}

	if p, _ := c.InspectPod(ctx, "demo--postgres"); p.State != "Running" {
		t.Fatalf("pod state = %q, want Running", p.State)
	}

	if err := c.DeletePod(ctx, "demo--postgres", nil); err != nil {
		t.Fatalf("DeletePod: %v", err)
	}

	if ok, _ := c.PodExists(ctx, "demo--postgres"); ok {
		t.Fatal("pod still exists after delete")
	}
}

func TestOpenShiftOnlyOperations(t *testing.T) {
	ctx := context.Background()

	podman := New(types.RuntimeTypePodman)
	if _, err := podman.ListRoutes(ctx, ""); err == nil {
		t.Error("podman mock: ListRoutes should be unsupported")
	}

	if _, err := podman.GetNamespace(ctx); err == nil {
		t.Error("podman mock: GetNamespace should be unsupported")
	}

	ocp := New(types.RuntimeTypeOpenShift)
	if ocp.Type() != types.RuntimeTypeOpenShift {
		t.Errorf("Type = %q", ocp.Type())
	}

	routes, err := ocp.ListRoutes(ctx, "")
	if err != nil {
		t.Errorf("openshift mock: ListRoutes: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes initially, got %d", len(routes))
	}

	ocp.InstallRelease("test-ns", "rel-1", "tmpl-1")
	routes, err = ocp.ListRoutes(ctx, "ai-services.io/service=rel-1")
	if err != nil {
		t.Fatalf("ListRoutes after install: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes for rel-1, got %d", len(routes))
	}

	ocp.UninstallRelease("test-ns", "rel-1")
	routes, err = ocp.ListRoutes(ctx, "ai-services.io/service=rel-1")
	if err != nil {
		t.Fatalf("ListRoutes after uninstall: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes after uninstall, got %d", len(routes))
	}

	if err := ocp.WaitForInferenceServiceReady(ctx, "llm"); err != nil {
		t.Errorf("openshift mock: WaitForInferenceServiceReady: %v", err)
	}
}

func TestGetSystemInfoReportsSpyreCards(t *testing.T) {
	info, err := New(types.RuntimeTypePodman).GetSystemInfo(context.Background())
	if err != nil {
		t.Fatalf("GetSystemInfo: %v", err)
	}

	acc := info.Accelerators[constants.SpyreResourceName]
	if info.CPU == nil || info.Memory == nil || acc == nil || acc.Available != acc.Total || acc.Total == 0 {
		t.Fatalf("unexpected system info: %+v", info)
	}
}

func TestEnabled(t *testing.T) {
	t.Setenv(EnvMockRuntime, "")

	if Enabled() {
		t.Error("Enabled() should be false when unset")
	}

	t.Setenv(EnvMockRuntime, "true")

	if !Enabled() {
		t.Error("Enabled() should be true when set to true")
	}
}

func TestCreatePodRecordsDeclaredVolumes(t *testing.T) {
	const manifest = `
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: standalone-pvc
---
apiVersion: v1
kind: Pod
metadata:
  name: translate-db
spec:
  volumes:
    - name: postgres-data
      persistentVolumeClaim:
        claimName: postgres-translate-abc
    - name: db-secret
      secret:
        secretName: translate-db-secret-abc
  containers:
    - name: postgres
      image: postgres:16
`
	ctx := context.Background()
	c := New(types.RuntimeTypePodman)

	if _, err := c.CreatePod(ctx, strings.NewReader(manifest), nil); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	for _, name := range []string{"standalone-pvc", "postgres-translate-abc", "translate-db-secret-abc"} {
		if ok, _ := c.VolumeExists(ctx, name); !ok {
			t.Errorf("volume %q not recorded", name)
		}
	}

	if ok, _ := c.VolumeExists(ctx, "never-created"); ok {
		t.Error("unexpected volume reported as existing")
	}
}

func TestHelmReleaseCreatesPodByTemplateID(t *testing.T) {
	ctx := context.Background()
	c := New(types.RuntimeTypeOpenShift)

	c.InstallRelease("ns", "app-chat", "tmpl-1")
	c.InstallRelease("ns", "app-chat", "tmpl-1") // upgrade must not duplicate
	c.InstallRelease("ns", "app-runtime", "")    // no template ID → no pod

	pods, err := c.ListPods(ctx, map[string][]string{"label": {constants.ApplicationTemplateKey + "=tmpl-1"}})
	if err != nil || len(pods) != 1 {
		t.Fatalf("pods = %v, err = %v; want exactly 1", pods, err)
	}

	pod := pods[0]
	if pod.Status != "Running" || len(pod.Containers) == 0 {
		t.Fatalf("unexpected pod: %+v", pod)
	}

	for _, ctr := range pod.Containers {
		// The catalog's OpenShift sync requires every container to be healthy.
		if ctr.Health != string(constants.Ready) {
			t.Errorf("container %q health = %q, want healthy", ctr.Name, ctr.Health)
		}
	}

	if all, _ := c.ListPods(ctx, nil); len(all) != 1 {
		t.Errorf("total pods = %d, want 1", len(all))
	}

	c.UninstallRelease("ns", "app-chat")

	if pods, _ := c.ListPods(ctx, nil); len(pods) != 0 {
		t.Errorf("pods after uninstall = %d, want 0", len(pods))
	}

	if c.ReleaseExists("ns", "app-chat") {
		t.Error("release still recorded after uninstall")
	}
}
