//go:build catalog_api
// +build catalog_api

package catalog

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// NewLoginCmd returns the cobra command for logging in to the catalog API server.
func NewLoginCmd() *cobra.Command {
	var (
		serverURL     string
		username      string
		passwordStdin bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to the catalog API server",
		Long: `Authenticate with the catalog API server using a username and password.

The generated access and refresh tokens are stored in the OS user config directory
and are used automatically by subsequent catalog commands. The exact path is
printed after a successful login.

The stored access token is reused for subsequent commands as long as it is still
valid. It is refreshed automatically only when it is about to expire, avoiding
unnecessary round-trips to the server.

Examples:
		# Interactive login (password is prompted securely)
		ai-services catalog login --server http://localhost:8080 --username admin

		# Non-interactive login via stdin pipe (password not recorded in shell history)
		echo "$MY_PASSWORD" | ai-services catalog login --server http://localhost:8080 --username admin --password-stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateServerURL(serverURL); err != nil {
				return err
			}

			password, err := promptPassword(passwordStdin)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Logging in to %s as %q...\n", serverURL, username)

			if _, err := client.NewWithLogin(serverURL, username, password); err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Login successful.")

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "http://localhost:8080", "Catalog API server URL")
	cmd.Flags().StringVar(&username, "username", "", "Username to authenticate with (required)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read password from stdin instead of an interactive prompt")

	_ = cmd.MarkFlagRequired("username")

	return cmd
}

// promptPassword reads the password from stdin if passwordStdin is true, or
// prompts the terminal securely otherwise. Returns an error if the read fails
// or the resulting password is empty.
func promptPassword(passwordStdin bool) (string, error) {
	var password string

	if passwordStdin {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			password = strings.TrimSpace(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
	} else {
		var err error
		password, err = readPasswordFromTerminal("Password: ")
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
	}

	if password == "" {
		return "", fmt.Errorf("password must not be empty")
	}

	return password, nil
}

// validateServerURL returns an error if raw is not a valid http or https URL.
func validateServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid --server URL %q: %w", raw, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid --server URL %q: scheme must be http or https", raw)
	}

	return nil
}

// readPasswordFromTerminal reads a password from the terminal without echoing it.
func readPasswordFromTerminal(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}

// Made with Bob
