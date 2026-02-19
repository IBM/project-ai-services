package openshift

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	ExpectedParts = 2
)

// OpenshiftClient implements the Runtime interface for Openshift.
type OpenshiftClient struct {
	clientset *kubernetes.Clientset
	namespace string
	ctx       context.Context
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

	kc, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}

	return &OpenshiftClient{
		clientset: clientset,
		namespace: namespace,
		ctx:       context.Background(),
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
			parts := strings.SplitN(lf, "=", ExpectedParts)
			if len(parts) == ExpectedParts {
				labels[parts[0]] = parts[1]
			}
		}
	}

	podList := &corev1.PodList{}
	err := kc.client.List(
		kc.ctx,
		podList,
		client.InNamespace(kc.namespace),
		labels,
	)
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
	logger.Warningln("not implemented")

	return nil, nil
}

// PodExists checks if a pod exists.
func (kc *OpenshiftClient) PodExists(nameOrID string) (bool, error) {
	pod := &corev1.Pod{}
	err := kc.client.Get(
		kc.ctx,
		client.ObjectKey{
			Namespace: kc.namespace,
			Name:      nameOrID,
		},
		pod,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		return false, err
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
	logger.Warningln("yet to implement")

	return nil
}

// ListContainers lists containers (returns pods' containers in Openshift).
func (kc *OpenshiftClient) ListContainers(filters map[string][]string) ([]types.Container, error) {
	logger.Warningln("not implemented")

	return nil, nil
}

// InspectContainer inspects a container.
func (kc *OpenshiftClient) InspectContainer(nameOrId string) (*types.Container, error) {
	logger.Warningln("not implemented")

	return nil, nil
}

// ContainerExists checks if a container exists.
func (kc *OpenshiftClient) ContainerExists(nameOrID string) (bool, error) {
	pods := &corev1.PodList{}
	err := kc.client.List(
		kc.ctx,
		pods,
		client.InNamespace(kc.namespace),
	)
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
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
	logger.Warningln("yet to implement")

	return nil
}

// Type returns the runtime type.
func (kc *OpenshiftClient) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}
