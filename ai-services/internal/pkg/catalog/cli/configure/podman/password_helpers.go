package podman

import (
	"fmt"
	"syscall"

	"github.com/charmbracelet/x/term"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// collectAndHashPassword collects the password from user and returns the hashed password.
// Returns empty string if the secret already exists (no password needed).
func collectAndHashPassword(rt runtime.Runtime) (string, error) {
	secretExists, err := rt.SecretExists(catalogconstants.CatalogSecretName)
	if err != nil {
		return "", fmt.Errorf("failed to check existing secrets: %w", err)
	}

	if secretExists {
		return "", nil
	}

	return promptAndHashPassword()
}

func promptAndHashPassword() (string, error) {
	// Prompt for admin password if secret doesn't exist
	adminPassword, err := promptForPassword()
	if err != nil {
		return "", fmt.Errorf("failed to read admin password: %w", err)
	}

	// Hash the password immediately after collection
	passwordHash, err := catalogutils.HashPasswordPBKDF2(adminPassword, defaultPasswordIterations)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return passwordHash, nil
}

// promptForPassword prompts the user to enter a password securely with confirmation.
func promptForPassword() (string, error) {
	password, err := readPasswordFromTerminal("Enter admin password: ")
	if err != nil {
		return "", err
	}

	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	// Prompt for confirmation
	confirm, err := readPasswordFromTerminal("Confirm admin password: ")
	if err != nil {
		return "", err
	}

	if password != confirm {
		return "", fmt.Errorf("passwords do not match")
	}

	return password, nil
}

// readPasswordFromTerminal reads a password from the terminal without echoing.
func readPasswordFromTerminal(prompt string) (string, error) {
	fmt.Print(prompt)
	passwordBytes, err := term.ReadPassword(uintptr(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		return "", err
	}

	return string(passwordBytes), nil
}

// Made with Bob
