package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"syscall"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"
)

const defaultPasswordIterations = 100000

// HashPasswordPBKDF2 generates a PBKDF2 hash of the password with a random salt.
// The hash is returned in the format: iterations.salt.hash (base64 encoded).
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

// CollectAndHashPassword collects the password from user and returns the hashed password.
// Returns empty string if the secret already exists (no password needed).
func CollectAndHashPassword(rt runtime.Runtime) (string, error) {
	secretExists, err := rt.SecretExists(catalogConstants.CatalogSecretName)
	if err != nil {
		return "", fmt.Errorf("failed to check existing secrets: %w", err)
	}

	if secretExists {
		return "", nil
	}

	return PromptAndHashPassword()
}

// PromptAndHashPassword prompts for a new password and returns its hash.
// Used for resets where the secret already exists.
func PromptAndHashPassword() (string, error) {
	adminPassword, err := promptForPassword()
	if err != nil {
		return "", fmt.Errorf("failed to read admin password: %w", err)
	}

	passwordHash, err := HashPasswordPBKDF2(adminPassword, defaultPasswordIterations)
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
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}

	return string(passwordBytes), nil
}

// Made with Bob
