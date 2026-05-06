package catalog

import (
	"fmt"
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
	// Base directory flag for catalog configure command.
	baseDir string
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

			var aiServicesDir string

			// Use default base directory if not specified, otherwise validate
			if baseDir == "" {
				aiServicesDir = constants.DefaultBaseDir
			} else {
				aiServicesDir, err = utils.ValidateBaseDir(baseDir)
				if err != nil {
					return fmt.Errorf("invalid base directory '%s': %w", baseDir, err)
				}
			}

			return configure.Run(configure.ConfigureOptions{
				AdminPassword: adminPassword,
				Runtime:       vars.RuntimeFactory.GetRuntimeType(),
				BaseDir:       aiServicesDir,
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

	logger.Infof("Using base directory: %s\n", baseDir, logger.VerbosityLevelDebug)

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

// configureConfigureFlags configures the flags for the configure command.
func configureConfigureFlags(cmd *cobra.Command, rawArgParams *[]string) {
	// Add runtime flag as required
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")

	// Add basedir flag
	cmd.Flags().StringVar(
		&baseDir,
		"basedir",
		"",
		"Base directory for AI services data (applications, models, cache).\n"+
			"Example: --basedir /custom/path\n",
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
