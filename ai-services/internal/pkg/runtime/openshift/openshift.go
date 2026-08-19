package openshift

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/accelerator/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNamespaceNotFound is returned by GetNamespace when the namespace does not exist.
var ErrNamespaceNotFound = errors.New("namespace not found")

var (
	scheme = runtime.NewScheme()

	// Singleton instances for all three clients, initialized together.
	clientsOnce sync.Once
	clientsErr  error

	controllerRuntimeClient client.Client
	kubeClient              *kubernetes.Clientset
	routeClient             *routeclient.Clientset
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
}

const (
	labelPartsCount = 2 // labelPartsCount is used to split label filters in the format "key=value".

	deleteNamespaceGracePeriod = int64(30)       // deleteNamespaceGracePeriod is the grace period in seconds for namespace deletion.
	deleteNamespaceTimeout     = 3 * time.Minute // deleteNamespaceTimeout is the maximum time to wait for a namespace deletion to complete.
)

// OpenshiftClient implements the Runtime interface for Openshift.
type OpenshiftClient struct {
	Client      client.Client
	KubeClient  *kubernetes.Clientset
	RouteClient *routeclient.Clientset
	Namespace   string
	Ctx         context.Context
}

// NewOpenshiftClient creates and returns an OpenshiftClient instance.
// The underlying clients (Client, KubeClient, RouteClient) are reused across all instances.
func NewOpenshiftClient() (*OpenshiftClient, error) {
	return NewOpenshiftClientWithNamespace("default")
}

// NewOpenshiftClientWithNamespace creates an OpenshiftClient with a specific namespace.
// The underlying clients (Client, KubeClient, RouteClient) are singletons and reused.
func NewOpenshiftClientWithNamespace(namespace string) (*OpenshiftClient, error) {
	// Initialize all three clients together (singleton pattern)
	if err := initializeClients(); err != nil {
		return nil, err
	}

	// Check if cluster is accessible
	if err := checkClusterAccessibility(); err != nil {
		return nil, fmt.Errorf("cluster is not accessible: %w", err)
	}

	return &OpenshiftClient{
		Client:      controllerRuntimeClient,
		KubeClient:  kubeClient,
		RouteClient: routeClient,
		Namespace:   namespace,
		Ctx:         context.Background(),
	}, nil
}

// initializeClients initializes all three clients once using sync.Once.
func initializeClients() error {
	clientsOnce.Do(func() {
		config, err := getKubeConfig()
		if err != nil {
			clientsErr = fmt.Errorf("failed to get openshift config: %w", err)

			return
		}

		// Initialize controller-runtime client
		controllerRuntimeClient, err = client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			clientsErr = fmt.Errorf("failed to create controller-runtime client: %w", err)

			return
		}

		// Initialize Kubernetes clientset
		kubeClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			clientsErr = fmt.Errorf("failed to create openshift clientset: %w", err)

			return
		}

		// Initialize OpenShift Route client
		routeClient, err = routeclient.NewForConfig(config)
		if err != nil {
			clientsErr = fmt.Errorf("failed to create openshift route clientset: %w", err)

			return
		}
	})

	return clientsErr
}

// checkClusterAccessibility verifies that the cluster is accessible by making a simple API call.
func checkClusterAccessibility() error {
	_, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	return nil
}

// getKubeConfig attempts to get openshift config from in-cluster or kubeconfig file.
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig file
	var kubeconfig string
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		kubeconfig = kubeconfigEnv
	} else if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}

// ListImages lists container images.
func (kc *OpenshiftClient) ListImages(_ context.Context) ([]types.Image, error) {
	logger.Warningln("ListImages is not implemented for OpenshiftClient. Returning empty list.")

	return []types.Image{}, nil
}

// PullImage pulls a container image.
func (kc *OpenshiftClient) PullImage(_ context.Context, image string) error {
	logger.Warningln("PullImage is not implemented for OpenshiftClient as image pulling is managed by kubelet.")

	return nil
}

// GetNamespace fetches the namespace details.
func (kc *OpenshiftClient) GetNamespace(ctx context.Context) (string, error) {
	ns, err := kc.KubeClient.CoreV1().Namespaces().Get(ctx, kc.Namespace, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", fmt.Errorf("%w: %s", ErrNamespaceNotFound, kc.Namespace)
		}

		return "", fmt.Errorf("failed to get namespace: %w", err)
	}

	return ns.Name, nil
}

// ListPods lists pods with optional filters.
func (kc *OpenshiftClient) ListPods(ctx context.Context, filters map[string][]string) ([]types.Pod, error) {
	labels := client.MatchingLabels{}
	if labelFilters, exists := filters["label"]; exists {
		for _, lf := range labelFilters {
			parts := strings.SplitN(lf, "=", labelPartsCount)
			if len(parts) == labelPartsCount {
				labels[parts[0]] = parts[1]
			}
		}
	}

	podList := &corev1.PodList{}
	err := kc.Client.List(ctx, podList, client.InNamespace(kc.Namespace), labels)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return toOpenshiftPodList(podList), nil
}

// CreatePod creates a pod from YAML manifest.
func (kc *OpenshiftClient) CreatePod(_ context.Context, body io.Reader, opts map[string]string) ([]types.Pod, error) {
	logger.Warningln("Not implemented")

	return nil, nil
}

// DeletePod deletes a pod by ID or name.
func (kc *OpenshiftClient) DeletePod(_ context.Context, id string, force *bool) error {
	logger.Warningln("Not implemented")

	return nil
}

// InspectPod inspects a pod and returns detailed information.
func (kc *OpenshiftClient) InspectPod(ctx context.Context, nameOrID string) (*types.Pod, error) {
	podName, err := getPodNameWithPrefix(kc, nameOrID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the pod: %w", err)
	}

	pod := &corev1.Pod{}
	err = kc.Client.Get(ctx, client.ObjectKey{
		Name:      podName,
		Namespace: kc.Namespace,
	}, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod from cluster: %w", err)
	}

	return toOpenshiftPod(pod), nil
}

// PodExists checks if a pod exists.
func (kc *OpenshiftClient) PodExists(_ context.Context, nameOrID string) (bool, error) {
	// Since OpenShift pod names have a random string added to it we cannot use Get() here.
	_, err := getPodNameWithPrefix(kc, nameOrID)
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	return true, nil
}

// StopPod stops a pod.
func (kc *OpenshiftClient) StopPod(_ context.Context, id string) error {
	logger.Warningf("Unsupported for openshift runtime")

	return nil
}

// StartPod starts a pod.
func (kc *OpenshiftClient) StartPod(_ context.Context, id string) error {
	logger.Warningf("Unsupported for openshift runtime")

	return nil
}

// PodLogs retrieves logs from a pod.
func (kc *OpenshiftClient) PodLogs(_ context.Context, podNameOrID string) error {
	podName, err := getPodNameWithPrefix(kc, podNameOrID)
	if err != nil {
		return fmt.Errorf("failed to get the pod: %w", err)
	}

	// Defaults to only container if there is one container in the pod.
	opts := &corev1.PodLogOptions{
		Follow: true,
	}

	return followLogs(kc, podName, opts)
}

// InspectContainer inspects a container.
func (kc *OpenshiftClient) InspectContainer(ctx context.Context, nameOrID string) (*types.Container, error) {
	pods := &corev1.PodList{}
	err := kc.Client.List(ctx, pods, client.InNamespace(kc.Namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.ContainerID == nameOrID || cs.Name == nameOrID {
				return toOpenShiftContainer(&cs, &pod), nil
			}
		}
	}

	return nil, fmt.Errorf("cannot find container: %s", nameOrID)
}

// ContainerExists checks if a container exists.
func (kc *OpenshiftClient) ContainerExists(ctx context.Context, nameOrID string) (bool, error) {
	// In Openshift, we check if any pod contains this container
	pods := &corev1.PodList{}
	err := kc.Client.List(ctx, pods, client.InNamespace(kc.Namespace))
	if err != nil {
		return false, fmt.Errorf("failed to check container: %w", err)
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == nameOrID {
				return true, nil
			}
		}
	}

	return false, nil
}

// ContainerLogs retrieves logs from a specific container.
func (kc *OpenshiftClient) ContainerLogs(ctx context.Context, containerNameOrID string) error {
	if containerNameOrID == "" {
		return fmt.Errorf("container name is required to fetch logs")
	}

	// In Openshift, we check if any pod contains this container
	pods := &corev1.PodList{}
	if err := kc.Client.List(ctx, pods, client.InNamespace(kc.Namespace)); err != nil {
		return fmt.Errorf("failed to check container: %w", err)
	}

	// Find pod containing the container
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == containerNameOrID {
				opts := &corev1.PodLogOptions{
					Container: containerNameOrID,
					Follow:    true,
				}

				return followLogs(kc, pod.Name, opts)
			}
		}
	}

	return fmt.Errorf("cannot find pod for the given container")
}

// ListCRD populates list resources based on input
// resources in the client namespace that carry every label key in filters["label"].
func (kc *OpenshiftClient) ListCRD(ctx context.Context, list *unstructured.UnstructuredList, filters map[string][]string) ([]types.CRDResource, error) {
	labelKeys := []string{}
	if labelFilters, exists := filters["label"]; exists {
		labelKeys = append(labelKeys, labelFilters...)
	}

	opts := []client.ListOption{
		client.InNamespace(kc.Namespace),
		client.HasLabels(labelKeys),
	}

	if err := kc.Client.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("failed to list CRD resources : %w", err)
	}

	result := make([]types.CRDResource, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, types.CRDResource{
			Name:   item.GetName(),
			Labels: item.GetLabels(),
		})
	}

	return result, nil
}

// ListRoutes lists routes in the namespace. Pass an empty string to list all routes.
func (kc *OpenshiftClient) ListRoutes(ctx context.Context, labelSelector string) ([]types.Route, error) {
	routeList, err := kc.RouteClient.RouteV1().Routes(kc.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	return toOpenShiftRouteList(routeList.Items), nil
}

// DeletePVCs deletes all PVCs matching the given application label.
func (kc *OpenshiftClient) DeletePVCs(ctx context.Context, appLabel string) error {
	pvcs, err := kc.KubeClient.CoreV1().PersistentVolumeClaims(kc.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel,
	})
	if err != nil {
		return fmt.Errorf("failed to list PVCs for cleanup: %w", err)
	}

	for _, pvc := range pvcs.Items {
		if err := kc.KubeClient.CoreV1().PersistentVolumeClaims(kc.Namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			logger.Warningf("Failed to delete PVC '%s': %v\n", pvc.Name, err)

			continue
		}

		logger.Debugf("Deleted PVC '%s'\n", pvc.Name)
	}

	return nil
}

// Type returns the runtime type.
func (kc *OpenshiftClient) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

func getPodNameWithPrefix(kc *OpenshiftClient, nameOrID string) (string, error) {
	pods, err := kc.ListPods(kc.Ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods {
		if pod.ID == nameOrID || strings.HasPrefix(pod.Name, nameOrID) {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("cannot find pod: %s", nameOrID)
}

func followLogs(kc *OpenshiftClient, podName string, opts *corev1.PodLogOptions) error {
	// Create interrupt-aware context (Ctrl+C)
	ctx, stop := signal.NotifyContext(kc.Ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	req := kc.KubeClient.CoreV1().Pods(kc.Namespace).GetLogs(podName, opts)

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}

	defer func() {
		if err := stream.Close(); err != nil {
			logger.Errorf("error closing log stream: %v", err)
		}
	}()

	scanner := bufio.NewScanner(stream)

	for scanner.Scan() {
		logger.Infoln(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return nil
		}

		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}

func (kc *OpenshiftClient) ListSecrets(_ context.Context, filters map[string][]string) ([]string, error) {
	logger.Warningln("Not implemented")

	return nil, nil
}

func (kc *OpenshiftClient) DeleteSecret(_ context.Context, name string) error {
	logger.Warningln("Not implemented")

	return nil
}

func (kc *OpenshiftClient) SecretExists(ctx context.Context, nameOrID string) (bool, error) {
	_, err := kc.KubeClient.CoreV1().Secrets(kc.Namespace).Get(ctx, nameOrID, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check secret existence: %w", err)
	}

	return true, nil
}

func (kc *OpenshiftClient) UpdateSecret(ctx context.Context, name, deploymentName string, data map[string][]byte) error {
	secretClient := kc.KubeClient.CoreV1().Secrets(kc.Namespace)

	existing, err := secretClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get existing secret: %w", err)
	}

	for k, v := range data {
		existing.Data[k] = v
	}

	_, err = secretClient.Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	if err := kc.rolloutRestartDeployment(deploymentName); err != nil {
		return fmt.Errorf("failed to restart deployment after secret update: %w", err)
	}

	const (
		pollInterval = 5 * time.Second
		pollTimeout  = 5 * time.Minute
	)

	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		ready, err := kc.isDeploymentReady(deploymentName)
		if err != nil {
			return fmt.Errorf("failed to check deployment readiness: %w", err)
		}
		if ready {
			return nil
		}
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timed out waiting for deployment %q to become ready after secret update", deploymentName)
}

func (kc *OpenshiftClient) DeleteVolume(_ context.Context, name string) error {
	logger.Warningln("Not implemented")

	return nil
}

func (kc *OpenshiftClient) VolumeExists(ctx context.Context, nameOrID string) (bool, error) {
	_, err := kc.KubeClient.CoreV1().PersistentVolumeClaims(kc.Namespace).Get(ctx, nameOrID, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check pvc existence: %w", err)
	}

	return true, nil
}

// GetSystemInfo returns cluster-level CPU, memory, and Spyre card availability.
//
// CPU and memory totals/available are fetched via Thanos PromQL:
//   - Total CPUs    : sum(kube_node_status_capacity{resource="cpu"})
//   - Available CPUs: sum(kube_node_status_allocatable{resource="cpu"})
//   - Total memory  : sum(kube_node_status_capacity{resource="memory"})
//   - Available mem : sum(kube_node_status_allocatable{resource="memory"})
//
// Spyre card counts are read directly from the Kubernetes API — see getSpyreCardInfo.
func (kc *OpenshiftClient) GetSystemInfo(ctx context.Context) (*models.SystemInfo, error) {
	sysInfo := &models.SystemInfo{
		Accelerators: make(map[string]*models.AcceleratorInfo),
	}

	// --- CPU ---
	totalCPU, err := queryThanos(ctx, `sum(kube_node_status_capacity{resource="cpu"})`)
	if err != nil {
		return nil, fmt.Errorf("failed to query total CPU capacity: %w", err)
	}

	availCPU, err := queryThanos(ctx, `sum(kube_node_status_allocatable{resource="cpu"})`)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocatable CPU: %w", err)
	}

	sysInfo.CPU = &models.CPUInfo{
		Total:     int(totalCPU),
		Available: availCPU,
	}

	// --- Memory ---
	totalMem, err := queryThanos(ctx, `sum(kube_node_status_capacity{resource="memory"})`)
	if err != nil {
		return nil, fmt.Errorf("failed to query total memory capacity: %w", err)
	}

	availMem, err := queryThanos(ctx, `sum(kube_node_status_allocatable{resource="memory"})`)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocatable memory: %w", err)
	}

	sysInfo.Memory = &models.MemoryInfo{
		TotalBytes:     int64(totalMem),
		AvailableBytes: int64(availMem),
	}

	// --- Spyre cards ---
	// Read directly from node capacity/allocatable so the count is accurate
	// even when Thanos has not yet scraped the custom resource metric.
	spyreInfo, err := kc.getSpyreCardInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Spyre card info: %w", err)
	}
	if spyreInfo != nil {
		sysInfo.Accelerators[constants.SpyreResourceName] = spyreInfo
	}

	return sysInfo, nil
}

// getSpyreCardInfo returns the total and available ibm.com/spyre_pf count for
// the cluster.
func (kc *OpenshiftClient) getSpyreCardInfo() (*models.AcceleratorInfo, error) {
	totalSpyre, err := kc.sumSpyreCapacity()
	if err != nil {
		return nil, err
	}

	if totalSpyre == 0 {
		return nil, nil //nolint:nilnil // no Spyre cards present — caller omits the key
	}

	usedSpyre, err := kc.sumSpyreInUse()
	if err != nil {
		return nil, err
	}

	available := totalSpyre - usedSpyre
	if available < 0 {
		available = 0
	}

	return &models.AcceleratorInfo{
		Total:     int(totalSpyre),
		Available: int(available),
	}, nil
}

// sumSpyreCapacity sums ibm.com/spyre_pf Capacity across all cluster nodes.
func (kc *OpenshiftClient) sumSpyreCapacity() (int64, error) {
	nodeList, err := kc.KubeClient.CoreV1().Nodes().List(kc.Ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list nodes: %w", err)
	}

	var total int64

	for i := range nodeList.Items {
		if qty, ok := nodeList.Items[i].Status.Capacity[corev1.ResourceName(constants.SpyreResourceName)]; ok {
			total += qty.Value()
		}
	}

	return total, nil
}

// sumSpyreInUse queries Thanos for the total number of ibm.com/spyre_pf units currently requested.
func (kc *OpenshiftClient) sumSpyreInUse() (int64, error) {
	used, err := queryThanos(kc.Ctx, `sum(kube_pod_container_resource_requests{resource="ibm_com_spyre_pf",container!=""})`)
	if err != nil {
		return 0, fmt.Errorf("failed to query Spyre cards in use: %w", err)
	}

	return int64(used), nil
}

// GetPodResources retrieves resource usage and Spyre card assignments for a pod.
func (kc *OpenshiftClient) GetPodResources(ctx context.Context, podName string) (*types.PodResources, error) {
	pod, err := kc.KubeClient.CoreV1().Pods(kc.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	spyreCards := []string{}
	collectSpyreCardsFromPod(kc, pod, &spyreCards)

	cpuQuery := fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container!=""}[5m]))`,
		kc.Namespace, podName,
	)

	cpuCores, err := queryThanos(ctx, cpuQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query CPU usage for pod %s: %w", podName, err)
	}

	memQuery := fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace=%q,pod=%q,container!=""})`,
		kc.Namespace, podName,
	)

	memBytes, err := queryThanos(ctx, memQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query memory usage for pod %s: %w", podName, err)
	}

	return &types.PodResources{
		CPU:        cpuCores,
		MemUsage:   uint64(memBytes),
		SpyreCards: spyreCards,
	}, nil
}

// collectSpyreCardsFromPod reads the PCIDEVICE_IBM_COM_AIU_PF environment variable
// from the live environment of each non-init container in the pod.
func collectSpyreCardsFromPod(kc *OpenshiftClient, pod *corev1.Pod, spyreCards *[]string) {
	for i := range pod.Spec.Containers {
		containerName := pod.Spec.Containers[i].Name

		output, err := kc.ExecInContainerWithCmd(kc.Ctx, pod.Name, containerName, []string{"env"})
		if err != nil {
			logger.Warningf("collectSpyreCardsFromPod: exec failed for container %s in pod %s/%s: %v", containerName, pod.Namespace, pod.Name, err)

			continue
		}

		addrs := spyre.ParseEnvVarAddresses(strings.Split(output, "\n"), string(constants.PCIDeviceEnvKey), ",")
		*spyreCards = append(*spyreCards, addrs...)
	}
}

// ExecInContainerWithCmd runs the given command inside the named container via
// the Kubernetes API server pod/exec subresource and returns the combined stdout.
func (kc *OpenshiftClient) ExecInContainerWithCmd(ctx context.Context, podName, containerName string, command []string) (string, error) {
	req := kc.KubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(kc.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdout:    true,
			Stderr:    false,
		}, clientgoscheme.ParameterCodec)

	config, err := getKubeConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get kube config for exec: %w", err)
	}

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor for %s/%s: %w", podName, containerName, err)
	}

	var stdout bytes.Buffer

	if err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
	}); err != nil {
		return "", fmt.Errorf("exec stream failed for %s/%s: %w", podName, containerName, err)
	}

	return stdout.String(), nil
}

// isDeploymentReady reports whether a rollout of the named deployment has fully completed.
func (kc *OpenshiftClient) isDeploymentReady(name string) (bool, error) {
	deployment, err := kc.KubeClient.AppsV1().Deployments(kc.Namespace).Get(kc.Ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get deployment %q: %w", name, err)
	}

	desired := int32(0)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	s := deployment.Status

	if s.ObservedGeneration < deployment.Generation {
		return false, nil
	}

	return s.UpdatedReplicas == desired &&
		s.Replicas == desired &&
		s.AvailableReplicas == desired, nil
}

// rolloutRestartDeployment triggers a rollout restart for the named deployment.
func (kc *OpenshiftClient) rolloutRestartDeployment(name string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}

	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal restart patch: %w", err)
	}

	_, err = kc.KubeClient.AppsV1().Deployments(kc.Namespace).Patch(
		kc.Ctx,
		name,
		k8stypes.MergePatchType,
		data,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to restart deployment %q: %w", name, err)
	}

	return nil
}

func (kc *OpenshiftClient) DeleteNamespace(ctx context.Context, name string) error {
	gracePeriod := deleteNamespaceGracePeriod
	propagation := metav1.DeletePropagationBackground

	timeoutCtx, cancel := context.WithTimeout(ctx, deleteNamespaceTimeout)
	defer cancel()

	err := kc.KubeClient.CoreV1().Namespaces().Delete(timeoutCtx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagation,
	})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.DebugfCtx(ctx, "Ignoring '%s' namespace deletion error: no namespace found\n", name)

			return nil
		}

		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}

	return nil
}
