package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	podmanTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// KubernetesClient implements the Runtime interface for Kubernetes.
type KubernetesClient struct {
	clientset *kubernetes.Clientset
	namespace string
	ctx       context.Context
}

// NewKubernetesClient creates and returns a new KubernetesClient instance.
func NewKubernetesClient() (*KubernetesClient, error) {
	return NewKubernetesClientWithNamespace("default")
}

// NewKubernetesClientWithNamespace creates a KubernetesClient with a specific namespace.
func NewKubernetesClientWithNamespace(namespace string) (*KubernetesClient, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &KubernetesClient{
		clientset: clientset,
		namespace: namespace,
		ctx:       context.Background(),
	}, nil
}

// getKubeConfig attempts to get kubernetes config from in-cluster or kubeconfig file.
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

// ListImages lists container images (Kubernetes doesn't have a direct equivalent)
// This is a placeholder that returns empty list.
func (kc *KubernetesClient) ListImages() ([]types.Image, error) {
	logger.Warningln("Not Implemented")

	return []types.Image{}, nil
}

// PullImage pulls a container image (handled by kubelet in Kubernetes).
func (kc *KubernetesClient) PullImage(image string) error {
	logger.Warningln("Not Implemented")

	return nil
}

// ListPods lists pods with optional filters.
func (kc *KubernetesClient) ListPods(filters map[string][]string) ([]types.Pod, error) {
	listOptions := metav1.ListOptions{}

	// Convert filters to label selector
	if labels, ok := filters["label"]; ok {
		listOptions.LabelSelector = strings.Join(labels, ",")
	}

	podList, err := kc.clientset.CoreV1().Pods(kc.namespace).List(kc.ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return toKubernetesPodsList(podList.Items), nil
}

// CreatePod creates a pod from YAML manifest.
func (kc *KubernetesClient) CreatePod(body io.Reader) ([]types.Pod, error) {
	logger.Warningln("Not implemented")

	return nil, nil
}

// DeletePod deletes a pod by ID or name.
func (kc *KubernetesClient) DeletePod(id string, force *bool) error {
	logger.Warningln("Not implemented")

	return nil
}

// InspectPod inspects a pod and returns detailed information.
func (kc *KubernetesClient) InspectPod(nameOrID string) (*podmanTypes.PodInspectReport, error) {
	logger.Warningln("not implemented")

	return nil, nil
}

// PodExists checks if a pod exists.
func (kc *KubernetesClient) PodExists(nameOrID string) (bool, error) {
	_, err := kc.clientset.CoreV1().Pods(kc.namespace).Get(kc.ctx, nameOrID, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// StopPod stops a pod.
func (kc *KubernetesClient) StopPod(id string) error {
	logger.Infof("not implemented")

	return nil
}

// StartPod starts a pod.
func (kc *KubernetesClient) StartPod(id string) error {
	logger.Warningf("not implemented")

	return nil
}

// PodLogs retrieves logs from a pod.
func (kc *KubernetesClient) PodLogs(podNameOrID string) error {
	pod, err := kc.clientset.CoreV1().Pods(kc.namespace).Get(kc.ctx, podNameOrID, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	// Get logs from all containers in the pod
	for _, container := range pod.Spec.Containers {
		logger.Infof("=== Logs from container: %s ===\n", container.Name)

		req := kc.clientset.CoreV1().Pods(kc.namespace).GetLogs(podNameOrID, &corev1.PodLogOptions{
			Container: container.Name,
			Follow:    true,
		})

		stream, err := req.Stream(kc.ctx)
		if err != nil {
			logger.Errorf("Failed to get logs for container %s: %v\n", container.Name, err)

			continue
		}
		defer func() {
			if err := stream.Close(); err != nil {
				logger.Errorf("Failed to close stream: %v", err)
			}
		}()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, stream)
		if err != nil {
			logger.Errorf("Failed to read logs for container %s: %v\n", container.Name, err)

			continue
		}

		logger.Infoln(buf.String())
	}

	return nil
}

// ListContainers lists containers (returns pods' containers in Kubernetes).
func (kc *KubernetesClient) ListContainers(filters map[string][]string) ([]types.Container, error) {
	logger.Warningln("not implemented")

	return nil, nil
}

// InspectContainer inspects a container.
func (kc *KubernetesClient) InspectContainer(nameOrId string) (*define.InspectContainerData, error) {
	logger.Warningln("not implemented")

	return nil, nil
}

// ContainerExists checks if a container exists.
func (kc *KubernetesClient) ContainerExists(nameOrID string) (bool, error) {
	// In Kubernetes, we check if any pod contains this container
	pods, err := kc.clientset.CoreV1().Pods(kc.namespace).List(kc.ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
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
func (kc *KubernetesClient) ContainerLogs(containerNameOrID string) error {
	// Find the pod containing this container
	pods, err := kc.clientset.CoreV1().Pods(kc.namespace).List(kc.ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == containerNameOrID {
				req := kc.clientset.CoreV1().Pods(kc.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
					Container: container.Name,
					Follow:    true,
				})

				stream, err := req.Stream(kc.ctx)
				if err != nil {
					return fmt.Errorf("failed to get logs: %w", err)
				}
				defer func() {
					if err := stream.Close(); err != nil {
						logger.Errorf("Failed to close stream: %v", err)
					}
				}()

				_, err = io.Copy(os.Stdout, stream)

				return err
			}
		}
	}

	return fmt.Errorf("container %s not found", containerNameOrID)
}

// Type returns the runtime type.
func (kc *KubernetesClient) Type() types.RuntimeType {
	return types.RuntimeTypeKubernetes
}

// Helper functions to convert Kubernetes types to runtime types

// func toRuntimePods(kubePods []corev1.Pod) []types.Pod {
// 	pods := make([]types.Pod, 0, len(kubePods))
// 	for _, kp := range kubePods {
// 		pods = append(pods, types.Pod{
// 			ID:     string(kp.UID),
// 			Name:   kp.Name,
// 			Status: string(kp.Status.Phase),
// 			Labels: kp.Labels,
// 			// Containers: toRuntimeContainers(kp),
// 		})
// 	}
//
// 	return pods
// }

// func toRuntimeContainers(pod corev1.Pod) []runtime.Container {
// 	containers := make([]runtime.Container, 0, len(pod.Status.ContainerStatuses))
// 	for _, cs := range pod.Status.ContainerStatuses {
// 		status := "unknown"
// 		if cs.State.Running != nil {
// 			status = "running"
// 		} else if cs.State.Waiting != nil {
// 			status = "waiting"
// 		} else if cs.State.Terminated != nil {
// 			status = "terminated"
// 		}

// 		containers = append(containers, runtime.Container{
// 			ID:     cs.ContainerID,
// 			Name:   cs.Name,
// 			Status: status,
// 		})
// 	}
// 	return containers
// }

func toKubernetesPodsList(kubePods []corev1.Pod) []types.Pod {
	pods := make([]types.Pod, 0, len(kubePods))
	for _, kp := range kubePods {
		pods = append(pods, types.Pod{
			ID:         string(kp.UID),
			Name:       kp.Name,
			Status:     string(kp.Status.Phase),
			Labels:     kp.Labels,
			Containers: toKubernetesContainersList(kp),
		})
	}

	return pods
}

func toKubernetesContainersList(pod corev1.Pod) []types.Container {
	containers := make([]types.Container, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		status := "unknown"
		if cs.State.Running != nil {
			status = "running"
		} else if cs.State.Waiting != nil {
			status = "waiting"
		} else if cs.State.Terminated != nil {
			status = "terminated"
		}

		containers = append(containers, types.Container{
			ID:     cs.ContainerID,
			Name:   cs.Name,
			Status: status,
		})
	}

	return containers
}

// Made with Bob
