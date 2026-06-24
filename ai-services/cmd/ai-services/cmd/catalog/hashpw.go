package catalog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	minIterations = 100000
)

func NewHashpwCmd() *cobra.Command {
	var (
		fromStdin   bool
		noConfirm   bool
		iterations  = 100000 // NIST recommended minimum
		runtimeType string
	)

	cmd := &cobra.Command{
		Use:   "hashpw",
		Short: "Generate a password hash",
		Long:  `Reads a password securely and prints a PBKDF2 hash to stdout.`,
		Example: `  # Interactive (hidden input, with confirmation)
  ai-services catalog hashpw --iterations 150000 --runtime podman

  # Non-interactive (CI): read from stdin
  printf '%s\n' 'S3cureP@ss!' | ai-services catalog hashpw --stdin --iterations 150000

Tip: Avoid passing plain passwords as CLI args (they can leak via process list).`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := getPassword(fromStdin, noConfirm, cmd)
			if err != nil {
				return err
			}

			if err := validateIterations(iterations); err != nil {
				return err
			}

			hash, err := catalogutils.HashPasswordPBKDF2(pw, iterations)
			if err != nil {
				return fmt.Errorf("pbkdf2: %w", err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(hash)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}

			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
	}

	cmd.Flags().IntVar(&iterations, "iterations", iterations, "PBKDF2 iterations (100000+ recommended)")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read password from stdin (non-interactive)")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip confirmation prompt")
	common.ConfigureRuntimeFlag(cmd, &runtimeType)

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
