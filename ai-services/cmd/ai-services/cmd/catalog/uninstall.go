package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Auto-yes flag for catalog uninstall command.
	uninstallAutoYes bool
)

// NewUninstallCmd creates a new uninstall command for the catalog service.
func NewUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the catalog service and clean up resources",
		Long: `Removes the catalog service and all associated resources including pods, secrets, and data.

Examples:
	 # Uninstall catalog service for podman
	 ai-services catalog uninstall --runtime podman
	 
	 # Uninstall without confirmation prompt
	 ai-services catalog uninstall --runtime podman -y`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return validateUninstallFlags()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return configure.Uninstall(configure.UninstallOptions{
				Runtime: vars.RuntimeFactory.GetRuntimeType(),
				AutoYes: uninstallAutoYes,
			})
		},
	}

	configureUninstallFlags(cmd)

	return cmd
}

// validateUninstallFlags validates the uninstall command flags and initializes runtime.
func validateUninstallFlags() error {
	// Initialize runtime factory based on flag
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

	if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
		return err
	}

	return nil
}

// configureUninstallFlags configures the flags for the uninstall command.
func configureUninstallFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")
	cmd.Flags().BoolVarP(&uninstallAutoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
}
