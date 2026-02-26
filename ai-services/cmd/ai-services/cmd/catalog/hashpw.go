//go:build catalog_api
// +build catalog_api

package catalog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

const (
	minIterations = 100000
)

func NewHashpwCmd() *cobra.Command {
	var (
		fromStdin  bool
		noConfirm  bool
		iterations = 100000 // NIST recommended minimum
	)

	cmd := &cobra.Command{
		Use:   "hashpw",
		Short: "Generate a password hash",
		Long: `Reads a password securely and prints a PBKDF2 hash to stdout.

Examples:
  # Interactive (hidden input, with confirmation)
  ai-services catalog hashpw --iterations 150000

  # Non-interactive (CI): read from stdin
  printf '%s\n' 'S3cureP@ss!' | ai-services catalog hashpw --stdin --iterations 150000

Tip: Avoid passing plain passwords as CLI args (they can leak via process list).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := getPassword(fromStdin, noConfirm, cmd)
			if err != nil {
				return err
			}

			if err := validateIterations(iterations); err != nil {
				return err
			}

			hash, err := hashPasswordPBKDF2(pw, iterations)
			if err != nil {
				return fmt.Errorf("pbkdf2: %w", err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(hash)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&iterations, "iterations", iterations, "PBKDF2 iterations (100000+ recommended)")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read password from stdin (non-interactive)")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip confirmation prompt")

	return cmd
}

func getPassword(fromStdin, noConfirm bool, cmd *cobra.Command) (string, error) {
	if fromStdin {
		return getPasswordFromStdin(cmd)
	}

	return getPasswordInteractive(noConfirm)
}

func getPasswordFromStdin(cmd *cobra.Command) (string, error) {
	b, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	pw := strings.TrimSpace(string(b))
	if pw == "" {
		return "", errors.New("empty password from stdin")
	}

	return pw, nil
}

func getPasswordInteractive(noConfirm bool) (string, error) {
	pw, err := readHidden("Password: ")
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if pw == "" {
		return "", errors.New("empty password")
	}

	if noConfirm {
		return pw, nil
	}

	confirm, err := readHidden("Confirm : ")
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}
	if confirm != pw {
		return "", errors.New("passwords do not match")
	}

	return pw, nil
}

func validateIterations(iter int) error {
	if iter < minIterations {
		return fmt.Errorf("invalid iterations=%d (must be > %d)", iter, minIterations)
	}

	return nil
}

func readHidden(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}

func hashPasswordPBKDF2(password string, iteration int) (string, error) {
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
