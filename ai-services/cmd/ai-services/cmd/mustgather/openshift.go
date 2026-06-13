package mustgather

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// OpenShiftMustGatherer implements MustGatherer for OpenShift runtime.
type OpenShiftMustGatherer struct {
	sanitizer *SecretSanitizer
	clientset *kubernetes.Clientset
}

// NewOpenShiftMustGatherer creates a new OpenShiftMustGatherer.
func NewOpenShiftMustGatherer() *OpenShiftMustGatherer {
	return &OpenShiftMustGatherer{
		sanitizer: NewSecretSanitizer(),
	}
}

// Gather collects debugging information for OpenShift runtime.
func (o *OpenShiftMustGatherer) Gather(opts MustGatherOptions) error {
	logger.Infoln("Starting must-gather for OpenShift runtime...")

	// Initialize Kubernetes client
	if err := o.initClient(); err != nil {
		return fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	// Create output directory
	outputDir, err := o.createOutputDirectory(opts.OutputDir)
	if err != nil {
		return err
	}

	logger.Infof("Output directory: %s\n", outputDir)

	// Get namespace
	namespace := o.getNamespace(opts.ApplicationName)

	// Collect all information
	o.collectAllResources(outputDir, namespace, opts.ApplicationName)

	logger.Infoln("Must-gather collection completed")

	return nil
}

// createOutputDirectory creates the output directory with a timestamp.
func (o *OpenShiftMustGatherer) createOutputDirectory(baseDir string) (string, error) {
	numericID := time.Now().UnixNano()
	outputDir := filepath.Join(baseDir, fmt.Sprintf("must-gather.local.%d", numericID))
	if err := os.MkdirAll(outputDir, dirPermissions); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	return outputDir, nil
}

// getNamespace determines the namespace to use.
func (o *OpenShiftMustGatherer) getNamespace(appName string) string {
	namespace := "default"
	if appName != "" {
		if ns := os.Getenv("NAMESPACE"); ns != "" {
			namespace = ns
		}
	}

	return namespace
}

// collectAllResources collects all Kubernetes resources.
func (o *OpenShiftMustGatherer) collectAllResources(outputDir, namespace, appName string) {
	// Collect cluster information
	if err := o.collectClusterInfo(outputDir); err != nil {
		logger.Warningf("Failed to collect cluster info: %v\n", err)
	}

	// Collect namespace information
	if err := o.collectNamespaceInfo(outputDir, namespace); err != nil {
		logger.Warningf("Failed to collect namespace info: %v\n", err)
	}

	// Collect pod information
	if err := o.collectPods(outputDir, namespace, appName); err != nil {
		logger.Warningf("Failed to collect pod info: %v\n", err)
	}

	// Collect events
	if err := o.collectEvents(outputDir, namespace); err != nil {
		logger.Warningf("Failed to collect events: %v\n", err)
	}

	// Collect services
	if err := o.collectServices(outputDir, namespace, appName); err != nil {
		logger.Warningf("Failed to collect services: %v\n", err)
	}

	// Collect deployments
	if err := o.collectDeployments(outputDir, namespace, appName); err != nil {
		logger.Warningf("Failed to collect deployments: %v\n", err)
	}

	// Collect configmaps (sanitized)
	if err := o.collectConfigMaps(outputDir, namespace, appName); err != nil {
		logger.Warningf("Failed to collect configmaps: %v\n", err)
	}

	// Collect routes
	if err := o.collectRoutes(outputDir, namespace, appName); err != nil {
		logger.Warningf("Failed to collect routes: %v\n", err)
	}
}

// initClient initializes the Kubernetes client.
func (o *OpenShiftMustGatherer) initClient() error {
	// Get Kubernetes config
	config, err := o.getKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	o.clientset = clientset

	return nil
}

// getKubeConfig returns the Kubernetes configuration.
func (o *OpenShiftMustGatherer) getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig file
	var kubeconfig string
	if home := os.Getenv("HOME"); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	// Use KUBECONFIG env var if set
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		kubeconfig = kubeconfigEnv
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}

// collectClusterInfo collects cluster-level information.
func (o *OpenShiftMustGatherer) collectClusterInfo(outputDir string) error {
	logger.Infoln("Collecting cluster information...")

	clusterDir := filepath.Join(outputDir, "cluster")
	if err := os.MkdirAll(clusterDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	// Get nodes
	nodes, err := o.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// Sanitize and save nodes
	sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", nodes))
	if err := o.writeFile(clusterDir, "nodes.txt", []byte(sanitized)); err != nil {
		return err
	}

	// Get namespaces
	namespaces, err := o.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	sanitized = o.sanitizer.SanitizeText(fmt.Sprintf("%+v", namespaces))
	if err := o.writeFile(clusterDir, "namespaces.txt", []byte(sanitized)); err != nil {
		return err
	}

	return nil
}

// collectNamespaceInfo collects namespace-specific information.
func (o *OpenShiftMustGatherer) collectNamespaceInfo(outputDir, namespace string) error {
	logger.Infof("Collecting namespace information for: %s\n", namespace)

	nsDir := filepath.Join(outputDir, "namespace")
	if err := os.MkdirAll(nsDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	// Get namespace details
	ns, err := o.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}

	sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", ns))
	if err := o.writeFile(nsDir, fmt.Sprintf("%s.txt", namespace), []byte(sanitized)); err != nil {
		return err
	}

	return nil
}

// collectPods collects pod information and logs.
func (o *OpenShiftMustGatherer) collectPods(outputDir, namespace, appName string) error {
	logger.Infoln("Collecting pod information...")

	podsDir := filepath.Join(outputDir, "pods")
	if err := os.MkdirAll(podsDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	// List pods
	pods, err := o.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		// Filter by application name if specified
		if appName != "" && !strings.Contains(pod.Name, appName) {
			continue
		}

		podDir := filepath.Join(podsDir, pod.Name)
		if err := os.MkdirAll(podDir, dirPermissions); err != nil {
			logger.Warningf("Failed to create directory for pod %s: %v\n", pod.Name, err)

			continue
		}

		// Save pod spec (sanitized)
		sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", pod))
		if err := o.writeFile(podDir, "spec.txt", []byte(sanitized)); err != nil {
			logger.Warningf("Failed to write pod spec for %s: %v\n", pod.Name, err)
		}

		// Collect logs from each container
		for _, container := range pod.Spec.Containers {
			o.collectContainerLogs(podDir, namespace, pod.Name, container.Name)
		}

		// Collect environment variables (sanitized)
		o.collectPodEnvVars(podDir, &pod)
	}

	return nil
}

// collectContainerLogs collects logs from a container.
func (o *OpenShiftMustGatherer) collectContainerLogs(podDir, namespace, podName, containerName string) {
	ctx := context.Background()

	// Get container logs
	tailLines := int64(maxLogLines)
	logOptions := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}

	logs, err := o.clientset.CoreV1().Pods(namespace).GetLogs(podName, logOptions).DoRaw(ctx)
	if err != nil {
		logger.Warningf("Failed to get logs for container %s in pod %s: %v\n", containerName, podName, err)

		return
	}

	// Sanitize and save logs
	sanitized := o.sanitizer.SanitizeText(string(logs))
	filename := fmt.Sprintf("%s.log", containerName)
	if err := o.writeFile(podDir, filename, []byte(sanitized)); err != nil {
		logger.Warningf("Failed to write logs for container %s: %v\n", containerName, err)
	}
}

// collectPodEnvVars collects and sanitizes environment variables from a pod.
func (o *OpenShiftMustGatherer) collectPodEnvVars(podDir string, pod *corev1.Pod) {
	envVars := make([]string, 0)

	for _, container := range pod.Spec.Containers {
		envVars = append(envVars, fmt.Sprintf("Container: %s", container.Name))
		for _, env := range container.Env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", env.Name, env.Value))
		}
		envVars = append(envVars, "")
	}

	// Sanitize environment variables
	sanitized := o.sanitizer.SanitizeEnvVars(envVars)
	content := strings.Join(sanitized, "\n")

	if err := o.writeFile(podDir, "env-vars.txt", []byte(content)); err != nil {
		logger.Warningf("Failed to write env vars for pod %s: %v\n", pod.Name, err)
	}
}

// collectEvents collects events from the namespace.
func (o *OpenShiftMustGatherer) collectEvents(outputDir, namespace string) error {
	logger.Infoln("Collecting events...")

	eventsDir := filepath.Join(outputDir, "events")
	if err := os.MkdirAll(eventsDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	events, err := o.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", events))
	if err := o.writeFile(eventsDir, "events.txt", []byte(sanitized)); err != nil {
		return err
	}

	return nil
}

// collectServices collects service information.
func (o *OpenShiftMustGatherer) collectServices(outputDir, namespace, appName string) error {
	logger.Infoln("Collecting services...")

	servicesDir := filepath.Join(outputDir, "services")
	if err := os.MkdirAll(servicesDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	services, err := o.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services.Items {
		// Filter by application name if specified
		if appName != "" && !strings.Contains(svc.Name, appName) {
			continue
		}

		sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", svc))
		filename := fmt.Sprintf("%s.txt", svc.Name)
		if err := o.writeFile(servicesDir, filename, []byte(sanitized)); err != nil {
			logger.Warningf("Failed to write service %s: %v\n", svc.Name, err)
		}
	}

	return nil
}

// collectDeployments collects deployment information.
func (o *OpenShiftMustGatherer) collectDeployments(outputDir, namespace, appName string) error {
	logger.Infoln("Collecting deployments...")

	deploymentsDir := filepath.Join(outputDir, "deployments")
	if err := os.MkdirAll(deploymentsDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	deployments, err := o.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, deploy := range deployments.Items {
		// Filter by application name if specified
		if appName != "" && !strings.Contains(deploy.Name, appName) {
			continue
		}

		sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", deploy))
		filename := fmt.Sprintf("%s.txt", deploy.Name)
		if err := o.writeFile(deploymentsDir, filename, []byte(sanitized)); err != nil {
			logger.Warningf("Failed to write deployment %s: %v\n", deploy.Name, err)
		}
	}

	return nil
}

// collectConfigMaps collects configmap information (sanitized).
func (o *OpenShiftMustGatherer) collectConfigMaps(outputDir, namespace, appName string) error {
	logger.Infoln("Collecting configmaps...")

	configMapsDir := filepath.Join(outputDir, "configmaps")
	if err := os.MkdirAll(configMapsDir, dirPermissions); err != nil {
		return err
	}

	ctx := context.Background()

	configMaps, err := o.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list configmaps: %w", err)
	}

	for _, cm := range configMaps.Items {
		// Filter by application name if specified
		if appName != "" && !strings.Contains(cm.Name, appName) {
			continue
		}

		sanitized := o.sanitizer.SanitizeText(fmt.Sprintf("%+v", cm))
		filename := fmt.Sprintf("%s.txt", cm.Name)
		if err := o.writeFile(configMapsDir, filename, []byte(sanitized)); err != nil {
			logger.Warningf("Failed to write configmap %s: %v\n", cm.Name, err)
		}
	}

	return nil
}

// collectRoutes collects route information.
func (o *OpenShiftMustGatherer) collectRoutes(outputDir, namespace, appName string) error {
	logger.Infoln("Collecting routes...")

	routesDir := filepath.Join(outputDir, "routes")
	if err := os.MkdirAll(routesDir, dirPermissions); err != nil {
		return err
	}

	// Note: Routes are OpenShift-specific and would require the OpenShift client
	// For now, we'll create a placeholder
	content := "Route collection requires OpenShift-specific client implementation"
	if err := o.writeFile(routesDir, "routes.txt", []byte(content)); err != nil {
		return err
	}

	return nil
}

// writeFile writes content to a file in the specified directory.
func (o *OpenShiftMustGatherer) writeFile(dir, filename string, content []byte) error {
	filepath := filepath.Join(dir, filename)

	return os.WriteFile(filepath, content, filePermissions)
}

// Made with Bob
