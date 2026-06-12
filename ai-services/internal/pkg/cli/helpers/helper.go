package helpers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"

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
func ListSpyreCards() ([]string, error) {
	return spyre.ListCards()
}

// FindFreeSpyreCards finds available (free) Spyre cards.
// This is a wrapper around spyre.FindFreeCards for backward compatibility.
func FindFreeSpyreCards() ([]string, error) {
	return spyre.FindFreeCards()
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
func CheckExistingResourcesForApplication(runtime runtime.Runtime, appName string, secretNames []string) ([]string, error) {
	// check existing pods for the application
	podsToSkip, err := existingPods(runtime, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing pods: %w", err)
	}

	// check existing secrets for the application
	secretsToSkip, err := existingSecrets(runtime, secretNames)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing secrets: %w", err)
	}

	resourcesToSkip := append(podsToSkip, secretsToSkip...)

	return resourcesToSkip, nil
}

func existingPods(runtime runtime.Runtime, appName string) ([]string, error) {
	//nolint:prealloc // as capacity is unknown and depends on runtime.ListPods response
	var podsToSkip []string
	pods, err := runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infof("No existing pods found for application: %s\n", appName)

		return nil, nil
	}

	logger.Infoln("Checking status of existing pods...")
	for _, pod := range pods {
		logger.Infof("Existing pod found: %s with status: %s\n", pod.Name, pod.Status)
		podsToSkip = append(podsToSkip, pod.Name)
	}

	return podsToSkip, nil
}

func existingSecrets(runtime runtime.Runtime, secretNames []string) ([]string, error) {
	secretsToSkip := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := runtime.ListSecrets(map[string][]string{
			"name": {secretName},
		})
		if err != nil && !strings.Contains(err.Error(), constants.ErrSecretNotFound) {
			return nil, fmt.Errorf("failed to list secrets: %w", err)
		}
		if len(secret) != 0 {
			logger.Infof("Existing secret found: %s\n", secret[0])
			secretsToSkip = append(secretsToSkip, secretName)
		}
	}

	return secretsToSkip, nil
}

// PromptForPassword prompts the user to enter a password securely.
func PromptForPassword() (string, error) {
	fmt.Print("Enter admin password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		return "", err
	}

	password := string(passwordBytes)
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	// Prompt for confirmation
	fmt.Print("Confirm admin password: ")
	confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		return "", err
	}

	confirm := string(confirmBytes)
	if password != confirm {
		return "", fmt.Errorf("passwords do not match")
	}

	return password, nil
}

// HashPasswordPBKDF2 generates a PBKDF2 hash of the password with a random salt.
func HashPasswordPBKDF2(password string, iteration int) (string, error) {
	salt := make([]byte, constants.Pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := pbkdf2.Key([]byte(password), salt, iteration, constants.Pbkdf2KeyLen, sha256.New)

	// Format: iterations.salt.hash (base64 encoded)
	encoded := fmt.Sprintf("%d.%s.%s",
		iteration,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))

	return encoded, nil
}

// GetAuthFilePath determines the auth.json file path.
func GetAuthFilePath() (string, error) {
	if os.Geteuid() == 0 {
		return "/run/user/0/containers/auth.json", nil
	}

	return fmt.Sprintf("/run/user/%d/containers/auth.json", os.Getuid()), nil
}
