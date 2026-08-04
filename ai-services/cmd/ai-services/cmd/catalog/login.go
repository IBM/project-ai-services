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

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewLoginCmd returns the cobra command for logging in to the catalog API server.
func NewLoginCmd() *cobra.Command {
	var (
		serverURL     string
		username      string
		passwordStdin bool
		insecure      bool
		runtimeType   string
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

To get the Catalog backend endpoint, use: ai-services catalog info`,
		Example: `  # Interactive login (password is prompted securely)
  ai-services catalog login --server <catalog_backend_endpoint> --username admin --runtime podman

  # Non-interactive login via stdin pipe (password not recorded in shell history)
  echo "$MY_PASSWORD" | ai-services catalog login --server <catalog_backend_endpoint> --username admin --password-stdin --runtime podman

  # Login with insecure TLS (skip certificate verification)
  ai-services catalog login --server <catalog_backend_endpoint> --username admin --insecure --runtime podman`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
				return err
			}

			return validateServerURL(serverURL)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(serverURL, username, passwordStdin, insecure)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Catalog backend endpoint (required)")
	cmd.Flags().StringVar(&username, "username", "", "Username to authenticate with (required)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read password from stdin instead of an interactive prompt")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification (NOT for production use)")
	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")

	return cmd
}

// runLogin executes the login flow with the provided parameters.
func runLogin(serverURL, username string, passwordStdin, insecure bool) error {
	password, err := promptPassword(passwordStdin)
	if err != nil {
		return err
	}

	// Warn user about insecure mode
	if insecure {
		logger.Warningln("WARNING: TLS certificate verification is disabled. This should NOT be used in production environments.")
	}

	logger.Infof("Logging in to %s as %q...\n", serverURL, username)

	if _, err := client.NewWithLogin(serverURL, username, password, insecure); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	logger.Infoln("Login successful.")

	return nil
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
