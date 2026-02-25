package catalog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func NewHashpwCmd() *cobra.Command {
	var (
		cost      int
		fromStdin bool
		noConfirm bool
	)

	cmd := &cobra.Command{
		Use:   "hashpw",
		Short: "Generate a bcrypt hash from a password",
		Long: `Reads a password securely and prints a bcrypt hash to stdout.

Examples:
  # Interactive (hidden input, with confirmation)
  ai-services catalog hashpw --cost 12

  # Non-interactive (CI): read from stdin
  printf '%s\n' 'S3cureP@ss!' | ai-services catalog hashpw --stdin --cost 12

Tip: Avoid passing plain passwords as CLI args (they can leak via process list).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := getPassword(fromStdin, noConfirm, cmd)
			if err != nil {
				return err
			}

			if err := validateCost(cost); err != nil {
				return err
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(pw), cost)
			if err != nil {
				return fmt.Errorf("bcrypt: %w", err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(hash)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&cost, "cost", bcrypt.DefaultCost, "bcrypt cost (10-14 typical; higher is slower & stronger)")
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

func validateCost(cost int) error {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return fmt.Errorf("invalid cost=%d (valid: %d..%d)", cost, bcrypt.MinCost, bcrypt.MaxCost)
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
