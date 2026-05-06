package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Runtime type flag for catalog configure command.
	runtimeType string
	// Application directory flag for catalog configure command.
	appDir string
)

// NewConfigureCmd creates a new configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	var (
		rawArgParams []string
		argParams    map[string]string
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the catalog service with initial configuration",
		Long: `Deploys the catalog service with the provided configuration.

Examples:
	 # Configure catalog service for podman
	 ai-services catalog configure --runtime podman
	 
	 # Configure with custom UI port
	 ai-services catalog configure --runtime podman --params ui.port=8081`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			var err error
			argParams, err = validateConfigureFlags(rawArgParams)

			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Prompt for admin password
			adminPassword, err := promptForPassword()
			if err != nil {
				return fmt.Errorf("failed to read admin password: %w", err)
			}

			// Add appDir to argParams
			if argParams == nil {
				argParams = make(map[string]string)
			}
			argParams["appdir"] = appDir

			return configure.Run(configure.ConfigureOptions{
				AdminPassword: adminPassword,
				Runtime:       vars.RuntimeFactory.GetRuntimeType(),
				ArgParams:     argParams,
			})
		},
	}

	configureConfigureFlags(cmd, &rawArgParams)

	return cmd
}

// validateConfigureFlags validates the configure command flags and initializes runtime.
func validateConfigureFlags(rawArgParams []string) (map[string]string, error) {
	// Initialize runtime factory based on flag
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return nil, fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

	// Check if podman runtime is being used on unsupported platform
	if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
		return nil, err
	}

	// Validate appDir permissions
	if err := validateAppDir(appDir); err != nil {
		return nil, fmt.Errorf("invalid app directory '%s': %w", appDir, err)
	}
	logger.Infof("Using app directory: %s\n", appDir, logger.VerbosityLevelDebug)

	// Parse params if provided
	var argParams map[string]string
	if len(rawArgParams) > 0 {
		var err error
		argParams, err = utils.ParseKeyValues(rawArgParams)
		if err != nil {
			return nil, fmt.Errorf("invalid params format: %w", err)
		}
	}

	return argParams, nil
}

// validateAppDir validates that the app directory exists or can be created and is writable.
func validateAppDir(dir string) error {
	// Clean the path
	dir = filepath.Clean(dir)

	// Check if directory exists or can be created
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}

	// Check write permissions by creating a test file
	testFile := filepath.Join(dir, ".ai-services-permission-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("no write permission: %w", err)
	}

	// Clean up test file
	if err := os.Remove(testFile); err != nil {
		logger.Warningf("Failed to remove test file %s: %v\n", testFile, err)
	}

	return nil
}

// configureConfigureFlags configures the flags for the configure command.
func configureConfigureFlags(cmd *cobra.Command, rawArgParams *[]string) {
	// Add runtime flag as required
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")

	// Add appdir flag with default
	cmd.Flags().StringVar(
		&appDir,
		"appdir",
		constants.DefaultAppDir,
		"Base directory for AI services data (applications, models, cache).\n\n"+
			fmt.Sprintf("Default: %s\n", constants.DefaultAppDir)+
			"Example: --appdir /custom/path/ai-services\n\n"+
			"The directory will be created if it doesn't exist.\n"+
			"User must have write permissions to this directory.\n",
	)

	cmd.Flags().StringSliceVar(
		rawArgParams,
		"params",
		[]string{},
		"Inline parameters to configure the catalog service.\n\n"+
			"Format:\n"+
			"- Comma-separated key=value pairs\n"+
			"- Example: --params ui.port=8081\n\n"+
			"Available parameters:\n"+
			"- ui.port: Port for the catalog UI (default: random available port)\n",
	)
}

// promptForPassword prompts the user to enter a password securely.
func promptForPassword() (string, error) {
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

// Made with Bob
