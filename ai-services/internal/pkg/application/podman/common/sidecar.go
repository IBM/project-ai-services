package common

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/bindings/secrets"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	secretKeyValueParts    = 2
	podmanExecPrefixLength = 2 // "exec" and containerID
)

// FindContainerAndPod finds the container and its pod ID using the template ID.
func FindContainerAndPod(ctx context.Context, templateID string) (string, string, error) {
	containerName, err := FindContainer(ctx, templateID)
	if err != nil {
		return "", "", fmt.Errorf("failed to find container: %w", err)
	}

	podID, err := GetPodID(ctx, containerName)
	if err != nil {
		return "", "", fmt.Errorf("failed to get pod ID: %w", err)
	}

	return containerName, podID, nil
}

// FindContainer finds the container for the given template ID using Podman SDK.
func FindContainer(ctx context.Context, templateID string) (string, error) {
	// Parse templateID as UUID to ensure it's valid
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		return "", fmt.Errorf("invalid template ID format: %w", err)
	}

	// List containers with filters
	filters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/template=%s", templateUUID.String())},
		"name":  {"opensearch"},
	}

	listOpts := &containers.ListOptions{}
	listOpts.WithFilters(filters)

	containerList, err := containers.List(ctx, listOpts)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containerList) == 0 {
		return "", fmt.Errorf("container not found for template ID: %s", templateUUID.String())
	}

	// Return the first matching container name
	return containerList[0].Names[0], nil
}

// GetPodID gets the pod ID for a container using Podman SDK.
func GetPodID(ctx context.Context, containerName string) (string, error) {
	// Inspect the container to get pod information
	containerData, err := containers.Inspect(ctx, containerName, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	podID := containerData.Pod
	if podID == "" {
		return "", fmt.Errorf("container is not part of a pod. Sidecar approach requires pod deployment")
	}

	return podID, nil
}

// CreateAndStartSidecar creates and starts a sidecar container.
func CreateAndStartSidecar(ctx context.Context, sidecarName, podID string) (string, error) {
	logger.Infoln("Starting sidecar container...")

	s := &specgen.SpecGenerator{
		ContainerBasicConfig: specgen.ContainerBasicConfig{
			Name:    sidecarName,
			Remove:  utils.BoolPtr(true), // Auto-remove container when stopped
			Command: []string{"sleep", "3600"},
			Pod:     podID,
		},
		ContainerStorageConfig: specgen.ContainerStorageConfig{
			Image: vars.ToolImage,
		},
		ContainerHealthCheckConfig: specgen.ContainerHealthCheckConfig{
			// Set HealthConfig to nil to disable health checks
			HealthConfig: nil,
			// Set HealthLogDestination to /tmp to satisfy directory requirement
			HealthLogDestination: "/tmp",
		},
	}

	createResponse, err := containers.CreateWithSpec(ctx, s, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create sidecar container: %w", err)
	}

	containerID := createResponse.ID
	if err := containers.Start(ctx, containerID, nil); err != nil {
		return "", fmt.Errorf("failed to start sidecar container: %w", err)
	}

	return containerID, nil
}

// ExecInContainer executes a command in a container using podman exec command.
func ExecInContainer(ctx context.Context, containerID string, cmd []string) error {
	// Build podman exec command with preallocated capacity
	args := make([]string, 0, podmanExecPrefixLength+len(cmd))
	args = append(args, "exec", containerID)
	args = append(args, cmd...)

	execCmd := exec.CommandContext(ctx, "podman", args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// GetOpenSearchPasswordFromSecret retrieves the OpenSearch password from the Podman secret using SDK.
func GetOpenSearchPasswordFromSecret(ctx context.Context, containerID string) (string, error) {
	secretName, err := getSecretNameFromContainer(ctx, containerID)
	if err != nil {
		return "", err
	}

	logger.Infof("Reading password from secret: %s\n", secretName)

	secretData, err := fetchSecretData(ctx, secretName)
	if err != nil {
		return "", err
	}

	password, err := extractPasswordFromSecretData(secretData)
	if err != nil {
		return "", err
	}

	logger.Infoln("Successfully retrieved password from secret")

	return password, nil
}

// getSecretNameFromContainer retrieves the secret name from the container's pod labels.
func getSecretNameFromContainer(ctx context.Context, containerID string) (string, error) {
	containerData, err := containers.Inspect(ctx, containerID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	podID := containerData.Pod
	if podID == "" {
		return "", fmt.Errorf("container is not part of a pod")
	}

	podData, err := pods.Inspect(ctx, podID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to inspect pod: %w", err)
	}

	secretName, ok := podData.Labels["ai-services.io/secret"]
	if !ok || secretName == "" {
		return "", fmt.Errorf("secret label 'ai-services.io/secret' not found in pod labels")
	}

	return secretName, nil
}

// fetchSecretData retrieves the secret data from Podman.
func fetchSecretData(ctx context.Context, secretName string) (string, error) {
	inspectOpts := &secrets.InspectOptions{}
	inspectOpts.WithShowSecret(true)

	secretInfo, err := secrets.Inspect(ctx, secretName, inspectOpts)
	if err != nil {
		return "", fmt.Errorf("failed to inspect secret %s: %w", secretName, err)
	}

	if secretInfo.SecretData == "" {
		return "", fmt.Errorf("secret data is empty for secret %s", secretName)
	}

	return secretInfo.SecretData, nil
}

// extractPasswordFromSecretData parses secret data to extract the password field.
func extractPasswordFromSecretData(secretData string) (string, error) {
	lines := strings.Split(secretData, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", secretKeyValueParts)
		if len(parts) != secretKeyValueParts {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "password" && value != "" {
			return value, nil
		}
	}

	return "", fmt.Errorf("password field not found in secret data")
}

// Made with Bob
