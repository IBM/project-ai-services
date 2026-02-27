package openshift

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
}

const (
	labelPartsCount = 2 // labelPartsCount is used to split label filters in the format "key=value".
)

// OpenshiftClient implements the Runtime interface for Openshift.
type OpenshiftClient struct {
	Client      client.Client
	KubeClient  *kubernetes.Clientset
	RouteClient *routeclient.Clientset
	Namespace   string
	Ctx         context.Context
}

// NewOpenshiftClient creates and returns a new OpenshiftClient instance.
func NewOpenshiftClient() (*OpenshiftClient, error) {
	return NewOpenshiftClientWithNamespace("default")
}

// NewOpenshiftClientWithNamespace creates a OpenshiftClient with a specific namespace.
func NewOpenshiftClientWithNamespace(namespace string) (*OpenshiftClient, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get openshift config: %w", err)
	}

	kcc, err := client.New(config, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create openshift clientset: %w", err)
	}

	// OpenShift Route client
	routeClient, err := routeclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create route clientset: %w", err)
	}

	return &OpenshiftClient{
		Client:      kcc,
		KubeClient:  kc,
		RouteClient: routeClient,
		Namespace:   namespace,
		Ctx:         context.Background(),
	}, nil
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
func (kc *OpenshiftClient) ListImages() ([]types.Image, error) {
	logger.Warningln("ListImages is not implemented for OpenshiftClient. Returning empty list.")

	return []types.Image{}, nil
}

// PullImage pulls a container image.
func (kc *OpenshiftClient) PullImage(image string) error {
	logger.Warningln("PullImage is not implemented for OpenshiftClient as image pulling is managed by kubelet.")

	return nil
}

// ListPods lists pods with optional filters.
func (kc *OpenshiftClient) ListPods(filters map[string][]string) ([]types.Pod, error) {
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
	err := kc.Client.List(kc.Ctx, podList, client.InNamespace(kc.Namespace), labels)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return toOpenshiftPodList(podList), nil
}

// CreatePod creates a pod from YAML manifest.
func (kc *OpenshiftClient) CreatePod(body io.Reader) ([]types.Pod, error) {
	logger.Warningln("Not implemented")

	return nil, nil
}

// DeletePod deletes a pod by ID or name.
func (kc *OpenshiftClient) DeletePod(id string, force *bool) error {
	logger.Warningln("Not implemented")

	return nil
}

// InspectPod inspects a pod and returns detailed information.
func (kc *OpenshiftClient) InspectPod(nameOrID string) (*types.Pod, error) {
	podName, err := getPodNameWithPrefix(kc, nameOrID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the pod: %w", err)
	}

	pod := &corev1.Pod{}
	err = kc.Client.Get(kc.Ctx, client.ObjectKey{
		Name:      podName,
		Namespace: kc.Namespace,
	}, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod from cluster: %w", err)
	}

	return toOpenshiftPod(pod), nil
}

// PodExists checks if a pod exists.
func (kc *OpenshiftClient) PodExists(nameOrID string) (bool, error) {
	// Since OpenShift pod names have a random string added to it we cannot use Get() here.
	_, err := getPodNameWithPrefix(kc, nameOrID)
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	return true, nil
}

// StopPod stops a pod.
func (kc *OpenshiftClient) StopPod(id string) error {
	logger.Infof("not implemented")

	return nil
}

// StartPod starts a pod.
func (kc *OpenshiftClient) StartPod(id string) error {
	logger.Warningf("not implemented")

	return nil
}

// PodLogs retrieves logs from a pod.
func (kc *OpenshiftClient) PodLogs(podNameOrID string) error {
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

// ListContainers lists containers (returns pods' containers in Openshift).
// func (kc *OpenshiftClient) ListContainers(filters map[string][]string) ([]types.Container, error) {
// 	logger.Warningln("not implemented")

// 	return nil, nil
// }

// InspectContainer inspects a container.
func (kc *OpenshiftClient) InspectContainer(nameOrID string) (*types.Container, error) {
	pods := &corev1.PodList{}
	err := kc.Client.List(kc.Ctx, pods, client.InNamespace(kc.Namespace))
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
func (kc *OpenshiftClient) ContainerExists(nameOrID string) (bool, error) {
	// In Openshift, we check if any pod contains this container
	pods := &corev1.PodList{}
	err := kc.Client.List(kc.Ctx, pods, client.InNamespace(kc.Namespace))
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
func (kc *OpenshiftClient) ContainerLogs(containerNameOrID string) error {
	if containerNameOrID == "" {
		return fmt.Errorf("container name is required to fetch logs")
	}

	// In Openshift, we check if any pod contains this container
	pods := &corev1.PodList{}
	if err := kc.Client.List(kc.Ctx, pods, client.InNamespace(kc.Namespace)); err != nil {
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

// ListRoutes lists all routes in the namespace.
func (kc *OpenshiftClient) ListRoutes() ([]types.Route, error) {
	routeList, err := kc.RouteClient.RouteV1().Routes(kc.Namespace).List(kc.Ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	return toOpenShiftRouteList(routeList.Items), nil
}

// DeletePVCs deletes all PVCs matching the given application label.
func (kc *OpenshiftClient) DeletePVCs(appLabel string) error {
	pvcs, err := kc.KubeClient.CoreV1().PersistentVolumeClaims(kc.Namespace).List(kc.Ctx, metav1.ListOptions{
		LabelSelector: appLabel,
	})
	if err != nil {
		return fmt.Errorf("failed to list PVCs for cleanup: %w", err)
	}

	for _, pvc := range pvcs.Items {
		if err := kc.KubeClient.CoreV1().PersistentVolumeClaims(kc.Namespace).Delete(kc.Ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			logger.Warningf("Failed to delete PVC '%s': %v\n", pvc.Name, err)

			continue
		}

		logger.Infof("Deleted PVC '%s'\n", pvc.Name, logger.VerbosityLevelDebug)
	}

	return nil
}

// Type returns the runtime type.
func (kc *OpenshiftClient) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

func getPodNameWithPrefix(kc *OpenshiftClient, nameOrID string) (string, error) {
	pods, err := kc.ListPods(nil)
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
