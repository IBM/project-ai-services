package common

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// FindOpenSearchPod finds the OpenSearch pod in OpenShift.
func FindOpenSearchPod(appID string) (string, string, error) {
	// Use app name as namespace (convention)
	namespace := appID

	// Find pod with vectordb component label
	cmd := exec.Command("oc", "get", "pods", "-n", namespace,
		"-l", fmt.Sprintf("ai-services.io/application=%s,ai-services.io/component=vectordb", appID),
		"-o", "jsonpath={.items[0].metadata.name}")

	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return "", "", fmt.Errorf("OpenSearch pod not found for app: %s in namespace: %s", appID, namespace)
	}

	podName := strings.TrimSpace(string(output))

	return namespace, podName, nil
}

// GetOpenSearchService gets the OpenSearch service name.
func GetOpenSearchService(appID, namespace string) (string, error) {
	cmd := exec.Command("oc", "get", "svc", "-n", namespace,
		"-l", fmt.Sprintf("ai-services.io/application=%s,ai-services.io/component=vectordb", appID),
		"-o", "jsonpath={.items[0].metadata.name}")

	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		// Default to "opensearch" if service not found
		return "opensearch", nil
	}

	return strings.TrimSpace(string(output)), nil
}

// CreateSidecarPod creates a sidecar pod in OpenShift.
// Returns a boolean indicating if the pod was created (for cleanup purposes) and an error.
func CreateSidecarPod(podName, namespace string) (bool, error) {
	podYAML := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: worker
    image: %s
    command: ["sleep", "3600"]
    securityContext:
      runAsUser: 0
  restartPolicy: Never
`, podName, namespace, vars.ToolImage)

	cmd := exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(podYAML)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to create sidecar pod: %w, output: %s", err, string(output))
	}

	// Pod is now created, so it needs cleanup even if subsequent steps fail
	podCreated := true

	// Wait for pod to be ready
	logger.Infoln("Waiting for pod to be ready...")
	cmd = exec.Command("oc", "wait", "--for=condition=Ready",
		fmt.Sprintf("pod/%s", podName), "-n", namespace, "--timeout=120s")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return podCreated, fmt.Errorf("pod failed to become ready: %w, output: %s", err, string(output))
	}

	logger.Infoln("Sidecar pod is ready")

	return podCreated, nil
}

// CleanupPod deletes an OpenShift pod.
func CleanupPod(podName, namespace string) {
	logger.Infoln("Cleaning up sidecar pod...")
	cmd := exec.Command("oc", "delete", "pod", podName, "-n", namespace, "--wait=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warningf("Failed to cleanup sidecar pod %s: %v, output: %s\n", podName, err, string(output))
	} else {
		logger.Infoln("Sidecar pod cleanup initiated")
	}
}

// GetOpenSearchPasswordFromSecret retrieves the OpenSearch password from the OpenShift secret.
func GetOpenSearchPasswordFromSecret(namespace string) (string, error) {
	// The secret name is "opensearch-credentials" in OpenShift
	secretName := "opensearch-credentials"

	// Get the secret using oc command
	cmd := exec.Command("oc", "get", "secret", secretName, "-n", namespace,
		"-o", "jsonpath={.data.password}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read secret %s: %w", secretName, err)
	}

	// The password is base64 encoded in the secret
	passwordBase64 := strings.TrimSpace(string(output))
	if passwordBase64 == "" {
		return "", fmt.Errorf("password not found in secret")
	}

	// Decode the base64 password
	passwordBytes, err := base64.StdEncoding.DecodeString(passwordBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode password: %w", err)
	}

	return string(passwordBytes), nil
}

// ExecInPod executes a command in an OpenShift pod.
func ExecInPod(podName, namespace, script string) error {
	cmd := exec.Command("oc", "exec", podName, "-n", namespace, "--", "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// ExecInPodWithOutput executes a command in an OpenShift pod and returns the output.
func ExecInPodWithOutput(podName, namespace, script string) (string, error) {
	cmd := exec.Command("oc", "exec", podName, "-n", namespace, "--", "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// GenerateSidecarName generates a unique sidecar pod name.
func GenerateSidecarName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().Unix())
}

// Made with Bob
