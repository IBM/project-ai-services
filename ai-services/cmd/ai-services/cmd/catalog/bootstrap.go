package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Runtime type flag for catalog bootstrap command.
	runtimeType string
)

// NewBootstrapCmd creates a new bootstrap command for the catalog service.
func NewBootstrapCmd() *cobra.Command {
	var (
		adminPassword string
		rawArgParams  []string
		argParams     map[string]string
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap the catalog service with initial configuration",
		Long: `Deploys the catalog service with the provided configuration.

Examples:
	 # Bootstrap catalog service for podman
	 ai-services catalog bootstrap --runtime podman --admin-password 'MySecurePassword123!'
	 
	 # Bootstrap with custom UI port
	 ai-services catalog bootstrap --runtime podman --admin-password 'MySecurePassword123!' --params ui.port=3000`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			var err error
			argParams, err = validateBootstrapFlags(adminPassword, rawArgParams)

			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return bootstrap.Run(bootstrap.BootstrapOptions{
				AdminPassword: adminPassword,
				Runtime:       vars.RuntimeFactory.GetRuntimeType(),
				ArgParams:     argParams,
			})
		},
	}

	configureBootstrapFlags(cmd, &adminPassword, &rawArgParams)

	return cmd
}

// validateBootstrapFlags validates the bootstrap command flags and initializes runtime.
func validateBootstrapFlags(adminPassword string, rawArgParams []string) (map[string]string, error) {
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

	if adminPassword == "" {
		return nil, fmt.Errorf("admin password is required (use --admin-password flag)")
	}

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

// configureBootstrapFlags configures the flags for the bootstrap command.
func configureBootstrapFlags(cmd *cobra.Command, adminPassword *string, rawArgParams *[]string) {
	// Add runtime flag as required
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")

	cmd.Flags().StringVar(adminPassword, "admin-password", "", "Password for the admin user (required)")
	cmd.Flags().StringSliceVar(
		rawArgParams,
		"params",
		[]string{},
		"Inline parameters to configure the catalog service.\n\n"+
			"Format:\n"+
			"- Comma-separated key=value pairs\n"+
			"- Example: --params ui.port=3000\n\n"+
			"Available parameters:\n"+
			"- ui.port: Port for the catalog UI (default: random available port)\n",
	)
}

// Made with Bob
