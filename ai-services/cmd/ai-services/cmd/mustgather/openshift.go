package mustgather

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	openshiftRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// openshiftGatherer collects must-gather data from an OpenShift cluster via the
// Kubernetes API (client-go / controller-runtime).
//
// Two namespace scopes are in play:
//   - catalogNS  ("ai-services")          — where the catalog backend/db/UI live.
//   - namespace  (--namespace flag value)  — the application namespace supplied
//     by the user, of the form "ai-services-<first 8 chars of UUID>".
//
// checkCatalogInstalled uses the catalog client (catalogNS). All other catalog
// artifact collection also targets catalogNS.
type openshiftGatherer struct {
	sanitizer *sanitize.SecretSanitizer
	namespace string // application namespace from --namespace flag
}

func newOpenshiftGatherer() *openshiftGatherer {
	return &openshiftGatherer{sanitizer: sanitize.NewSecretSanitizer()}
}

// ── entry point ───────────────────────────────────────────────────────────────

// gather creates a timestamped output directory and runs every collection step.
//
// Collection is split into two tiers:
//   - Catalog-dependent: catalog pods, app pods, catalog credentials — skipped
//     when no catalog pods exist in the catalog namespace.
//   - Always-on: system info, K8s secrets, services/routes, PVCs, events —
//     collected from the application namespace regardless of catalog state.
func (g *openshiftGatherer) gather(opts gatherOptions) (string, error) {
	ctx := context.Background()
	g.namespace = opts.namespace

	logger.InfolnCtx(ctx, "Starting must-gather for OpenShift runtime…")

	// catalogClient is scoped to the fixed catalog namespace ("ai-services").
	catalogClient, err := openshiftRuntime.NewOpenshiftClientWithNamespace(catalogConstants.CatalogAppName)
	if err != nil {
		return "", fmt.Errorf("failed to connect to OpenShift cluster: %w", err)
	}

	// appClient is scoped to the application namespace provided by --namespace.
	appClient, err := openshiftRuntime.NewOpenshiftClientWithNamespace(g.namespace)
	if err != nil {
		return "", fmt.Errorf("failed to connect to OpenShift cluster: %w", err)
	}

	outDir, err := createOutputDir(opts.outputDir)
	if err != nil {
		return "", err
	}

	logger.InfofCtx(ctx, "Output directory: %s\n", outDir)
	logger.InfofCtx(ctx, "Catalog namespace: %s\n", catalogConstants.CatalogAppName)
	logger.InfofCtx(ctx, "Application namespace: %s\n", g.namespace)

	catalogInstalled, err := checkCatalogInstalled(catalogClient)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to check catalog installation: %v\n", err)
	}

	if catalogInstalled {
		g.collectCatalogArtifacts(ctx, catalogClient, outDir)
		collectApplicationPods(ctx, g, outDir, opts.applicationName)
	} else {
		logger.WarninglnCtx(ctx, "No catalog pods found in namespace "+catalogConstants.CatalogAppName+" — catalog is not installed. Skipping catalog artifacts and application pod collection.")
	}

	// Always collected
	g.collectSystemInfo(ctx, appClient, outDir)
	g.collectEventInfo(ctx, appClient, outDir)
	// Secrets are collected from both namespaces into separate files
	g.collectSecretInfo(ctx, catalogClient, "catalog-secrets.json", outDir)
	g.collectSecretInfo(ctx, appClient, "secrets.json", outDir)
	g.collectNetworkInfo(ctx, appClient, outDir)
	// PVCs are collected from both namespaces into separate files
	g.collectVolumeInfo(ctx, catalogClient, "catalog-pvcs.json", outDir)
	g.collectVolumeInfo(ctx, appClient, "pvcs.json", outDir)

	return outDir, nil
}

// ── catalog artifact collection ───────────────────────────────────────────────

// collectCatalogArtifacts collects catalog pods (inspect + logs) and the local catalog-credentials.json.
func (g *openshiftGatherer) collectCatalogArtifacts(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, outDir string) {
	logger.InfolnCtx(ctx, "Collecting catalog artifacts…")

	catDir := filepath.Join(outDir, "catalog")
	if err := os.MkdirAll(catDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create catalog directory: %v\n", err)

		return
	}

	g.collectCatalogPods(ctx, rt, catDir)
	collectCatalogCredentials(ctx, g.sanitizer, catDir)
}

// collectCatalogPods lists all pods labelled ai-services.io/application=ai-services
// in the catalog namespace and collects inspect + logs for each.
func (g *openshiftGatherer) collectCatalogPods(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, catDir string) {
	pods, err := rt.ListPods(map[string][]string{
		"label": {"ai-services.io/application=" + catalogConstants.CatalogAppName},
	})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list catalog pods: %v\n", err)

		return
	}

	if len(pods) == 0 {
		logger.WarninglnCtx(ctx, "No catalog pods found (catalog may not be configured).")

		return
	}

	podsDir := filepath.Join(catDir, "pods")
	if err := os.MkdirAll(podsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create catalog pods directory: %v\n", err)

		return
	}

	for _, pod := range pods {
		// Catalog pods live in the catalog namespace.
		g.collectPod(ctx, podsDir, pod.Name, catalogConstants.CatalogAppName)
	}
}

// ── per-pod collection ────────────────────────────────────────────────────────

// collectPod collects the pod manifest (as JSON) and logs for every container.
func (g *openshiftGatherer) collectPod(ctx context.Context, podsDir, podName, namespace string) {
	if namespace == "" {
		logger.WarningfCtx(ctx, "Cannot collect pod %q: namespace is empty (app UUID may be invalid)\n", podName)

		return
	}

	rt, err := openshiftRuntime.NewOpenshiftClientWithNamespace(namespace)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to create client for namespace %q (pod %q): %v\n", namespace, podName, err)

		return
	}

	podDir := filepath.Join(podsDir, podName)
	if err := os.MkdirAll(podDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create directory for pod %q: %v\n", podName, err)

		return
	}

	g.collectPodInspect(ctx, rt, podDir, podName)
	g.collectContainersForPod(ctx, rt, podDir, podName)
}

// collectPodInspect fetches the full pod object and writes it as sanitized JSON.
func (g *openshiftGatherer) collectPodInspect(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, podDir, podName string) {
	pod, err := rt.KubeClient.CoreV1().Pods(rt.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to inspect pod %q: %v\n", podName, err)

		return
	}

	raw, err := json.Marshal(pod)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to marshal pod %q: %v\n", podName, err)

		return
	}

	writeFile(ctx, podDir, "inspect.json", g.sanitizer.SanitizeJSON(raw))
}

// collectContainersForPod writes a per-container inspect JSON and log file for
// every container in the pod (init containers excluded).
func (g *openshiftGatherer) collectContainersForPod(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, podDir, podName string) {
	pod, err := rt.KubeClient.CoreV1().Pods(rt.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to get containers for pod %q: %v\n", podName, err)

		return
	}

	for i := range pod.Spec.Containers {
		name := pod.Spec.Containers[i].Name
		g.collectContainerInspect(ctx, pod, podDir, name)
		g.collectContainerLogs(ctx, rt, podDir, podName, name)
	}
}

// collectContainerInspect writes the container spec + status slice for one
// container as sanitized JSON under podDir/inspect/<name>.json.
func (g *openshiftGatherer) collectContainerInspect(ctx context.Context, pod *corev1.Pod, podDir, containerName string) {
	// Gather the matching ContainerStatus (may not exist if pod never started).
	var cs *corev1.ContainerStatus
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == containerName {
			cs = &pod.Status.ContainerStatuses[i]

			break
		}
	}

	payload := map[string]any{
		"spec":   findContainerSpec(pod, containerName),
		"status": cs,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to marshal inspect for container %q: %v\n", containerName, err)

		return
	}

	inspectDir := filepath.Join(podDir, "inspect")
	if err := os.MkdirAll(inspectDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create inspect directory: %v\n", err)

		return
	}

	writeFile(ctx, inspectDir, containerName+".json", g.sanitizer.SanitizeJSON(raw))
}

// collectContainerLogs fetches the last maxLogLines lines from containerName
// inside podName and writes them to podDir/logs/<containerName>.log.
func (g *openshiftGatherer) collectContainerLogs(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, podDir, podName, containerName string) {
	logsDir := filepath.Join(podDir, "logs")
	if err := os.MkdirAll(logsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create logs directory: %v\n", err)

		return
	}

	tail, _ := strconv.ParseInt(maxLogLines, 10, 64)
	raw, err := rt.KubeClient.CoreV1().Pods(rt.Namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tail,
	}).DoRaw(ctx)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to get logs for container %q in pod %q: %v\n", containerName, podName, err)

		return
	}

	writeFile(ctx, logsDir, containerName+".log", g.sanitizer.SanitizeText(raw))
}

// findContainerSpec returns the ContainerSpec for containerName from a pod, or nil.
func findContainerSpec(pod *corev1.Pod, containerName string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == containerName {
			return &pod.Spec.Containers[i]
		}
	}

	return nil
}

// ── system info ───────────────────────────────────────────────────────────────

// collectSystemInfo writes cluster server version and node list.
func (g *openshiftGatherer) collectSystemInfo(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, outDir string) {
	logger.InfolnCtx(ctx, "Collecting system information…")

	sysDir := filepath.Join(outDir, "system")
	if err := os.MkdirAll(sysDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create system directory: %v\n", err)

		return
	}

	// Cluster server version.
	ver, err := rt.KubeClient.Discovery().ServerVersion()
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to get server version: %v\n", err)
	} else {
		raw, _ := json.Marshal(ver)
		writeFile(ctx, sysDir, "version.json", g.sanitizer.SanitizeJSON(raw))
	}

	// Node list.
	nodes, err := rt.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list nodes: %v\n", err)
	} else {
		raw, _ := json.Marshal(nodes)
		writeFile(ctx, sysDir, "nodes.json", g.sanitizer.SanitizeJSON(raw))
	}
}

// ── events ────────────────────────────────────────────────────────────────────

// collectEventInfo writes all Kubernetes Events from the application namespace.
func (g *openshiftGatherer) collectEventInfo(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, outDir string) {
	logger.InfolnCtx(ctx, "Collecting events…")

	eventsDir := filepath.Join(outDir, "events")
	if err := os.MkdirAll(eventsDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create events directory: %v\n", err)

		return
	}

	events, err := rt.KubeClient.CoreV1().Events(rt.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list events: %v\n", err)

		return
	}

	raw, err := json.Marshal(events)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to marshal events: %v\n", err)

		return
	}

	writeFile(ctx, eventsDir, "events.json", g.sanitizer.SanitizeJSON(raw))
}

// ── secrets ───────────────────────────────────────────────────────────────────

// collectSecretInfo lists all K8s Secrets from rt's namespace and writes them
// to secrets/<filename>. The data and stringData fields are stripped entirely
// before writing, only metadata (name, type, labels, annotations,
// creationTimestamp) is kept.
func (g *openshiftGatherer) collectSecretInfo(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, filename, outDir string) {
	logger.InfolnCtx(ctx, "Collecting secret metadata…")

	secDir := filepath.Join(outDir, "secrets")
	if err := os.MkdirAll(secDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create secrets directory: %v\n", err)

		return
	}

	secrets, err := rt.KubeClient.CoreV1().Secrets(rt.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list secrets in namespace %q: %v\n", rt.Namespace, err)

		return
	}

	// Strip secret values before marshalling — never expose base64-encoded data.
	for i := range secrets.Items {
		secrets.Items[i].Data = nil
		secrets.Items[i].StringData = nil
	}

	raw, err := json.Marshal(secrets)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to marshal secrets in namespace %q: %v\n", rt.Namespace, err)

		return
	}

	writeFile(ctx, secDir, filename, g.sanitizer.SanitizeJSON(raw))
}

// ── network ───────────────────────────────────────────────────────────────────

// collectNetworkInfo writes K8s Services and OpenShift Routes from the
// application namespace.
func (g *openshiftGatherer) collectNetworkInfo(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, outDir string) {
	logger.InfolnCtx(ctx, "Collecting network information…")

	netDir := filepath.Join(outDir, "network")
	if err := os.MkdirAll(netDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create network directory: %v\n", err)

		return
	}

	// Services.
	svcs, err := rt.KubeClient.CoreV1().Services(rt.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list services: %v\n", err)
	} else {
		raw, _ := json.Marshal(svcs)
		writeFile(ctx, netDir, "services.json", g.sanitizer.SanitizeJSON(raw))
	}

	// OpenShift Routes.
	routes, err := rt.RouteClient.RouteV1().Routes(rt.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list routes: %v\n", err)
	} else {
		raw, _ := json.Marshal(routes)
		writeFile(ctx, netDir, "routes.json", g.sanitizer.SanitizeJSON(raw))
	}
}

// ── volumes ───────────────────────────────────────────────────────────────────

// collectVolumeInfo writes all PersistentVolumeClaims from rt's namespace to
// volumes/<filename>.
func (g *openshiftGatherer) collectVolumeInfo(ctx context.Context, rt *openshiftRuntime.OpenshiftClient, filename, outDir string) {
	logger.InfolnCtx(ctx, "Collecting volume information…")

	volDir := filepath.Join(outDir, "volumes")
	if err := os.MkdirAll(volDir, dirPerm); err != nil {
		logger.WarningfCtx(ctx, "Failed to create volumes directory: %v\n", err)

		return
	}

	pvcs, err := rt.KubeClient.CoreV1().PersistentVolumeClaims(rt.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to list PVCs in namespace %q: %v\n", rt.Namespace, err)

		return
	}

	raw, err := json.Marshal(pvcs)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to marshal PVCs in namespace %q: %v\n", rt.Namespace, err)

		return
	}

	writeFile(ctx, volDir, filename, g.sanitizer.SanitizeJSON(raw))
}
