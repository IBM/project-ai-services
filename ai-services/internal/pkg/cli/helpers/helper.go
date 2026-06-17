package helpers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/accelerator/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

const (
	inspectPollInterval = 10 * time.Second
)

func WaitForContainerReadiness(runtime runtime.Runtime, containerNameOrId string, timeout time.Duration) error {
	var containerStatus *types.Container
	var err error

	deadline := time.Now().Add(timeout)

	for {
		// fetch the container status
		containerStatus, err = runtime.InspectContainer(containerNameOrId)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		healthStatus := containerStatus.Health

		if healthStatus == "" {
			return nil
		}

		if healthStatus == string(constants.Ready) {
			return nil
		}

		// if deadline exceeds, stop the container readiness check
		if time.Now().After(deadline) {
			return fmt.Errorf("operation timed out waiting for container readiness")
		}

		// every 10 seconds inspect the container
		time.Sleep(inspectPollInterval)
	}
}

// WaitForContainersCreation waits until all the containers in the provided podID are created within the specified timeout.
func WaitForContainersCreation(runtime runtime.Runtime, podID string, expectedContainerCount int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		// fetch the pod info
		pInfo, err := runtime.InspectPod(podID)
		if err != nil {
			return fmt.Errorf("failed to do pod inspect for podID: %s with error: %w", podID, err)
		}

		// if the expected count is reached, then all the containers are created
		// Note: Adding +1 to the expectedContainerCount as there is an additional 'infra' container added to all pods by podman
		if len(pInfo.Containers) == expectedContainerCount+1 {
			return nil
		}

		// if deadline exceeds, stop the container creation check
		if time.Now().After(deadline) {
			return fmt.Errorf("operation timed out waiting for container creation")
		}

		// every 10 seconds inspect the pod
		time.Sleep(inspectPollInterval)
	}
}

func FetchContainerStartPeriod(runtime runtime.Runtime, containerNameOrId string) (time.Duration, error) {
	// fetch the container stats
	containerStats, err := runtime.InspectContainer(containerNameOrId)
	if err != nil {
		return 0, fmt.Errorf("failed to check container stats: %w", err)
	}

	return containerStats.HealthcheckStartPeriod, nil
}

// ListSpyreCards lists all Spyre cards attached to the system.
// This is a wrapper around spyre.ListCards for backward compatibility.
func ListSpyreCards(ctx context.Context) ([]string, error) {
	return spyre.ListCards(ctx)
}

// FindFreeSpyreCards finds available (free) Spyre cards.
// This is a wrapper around spyre.FindFreeCards for backward compatibility.
func FindFreeSpyreCards(ctx context.Context) ([]string, error) {
	return spyre.FindFreeCards(ctx)
}

func ParseSkipChecks(skipChecks []string) map[string]bool {
	skipMap := make(map[string]bool)
	for _, check := range skipChecks {
		parts := strings.Split(check, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(strings.ToLower(part))
			if trimmed != "" {
				skipMap[trimmed] = true
			}
		}
	}

	return skipMap
}

// CheckExistingResourcesForApplication checks if there are resources already existing for the given application name.
func CheckExistingResourcesForApplication(ctx context.Context, runtime runtime.Runtime, appName string, secretNames []string) ([]string, error) {
	// check existing pods for the application
	podsToSkip, err := existingPods(ctx, runtime, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing pods: %w", err)
	}

	// check existing secrets for the application
	secretsToSkip, err := existingSecrets(ctx, runtime, secretNames)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing secrets: %w", err)
	}

	resourcesToSkip := append(podsToSkip, secretsToSkip...)

	return resourcesToSkip, nil
}

func existingPods(ctx context.Context, runtime runtime.Runtime, appName string) ([]string, error) {
	//nolint:prealloc // as capacity is unknown and depends on runtime.ListPods response
	var podsToSkip []string
	pods, err := runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		// No existing pods found for application
		return nil, nil
	}

	logger.InfolnCtx(ctx, "Checking status of existing pods...")
	for _, pod := range pods {
		logger.InfofCtx(ctx, "Existing pod found: %s with status: %s\n", pod.Name, pod.Status)
		podsToSkip = append(podsToSkip, pod.Name)
	}

	return podsToSkip, nil
}

func existingSecrets(ctx context.Context, runtime runtime.Runtime, secretNames []string) ([]string, error) {
	secretsToSkip := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := runtime.ListSecrets(map[string][]string{
			"name": {secretName},
		})
		if err != nil && !strings.Contains(err.Error(), constants.ErrSecretNotFound) {
			return nil, fmt.Errorf("failed to list secrets: %w", err)
		}
		if len(secret) != 0 {
			logger.InfofCtx(ctx, "Existing secret found: %s\n", secret[0])
			secretsToSkip = append(secretsToSkip, secretName)
		}
	}

	return secretsToSkip, nil
}
