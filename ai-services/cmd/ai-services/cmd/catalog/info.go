package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/info"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// NewInfoCmd creates a new info command for the catalog service.
func NewInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display catalog service information",
		Long: `Displays information about the running catalog service, including:
- Catalog service version
- Catalog UI endpoint
- Catalog Backend API endpoint

Examples:
	# Display catalog service info for podman
	ai-services catalog info --runtime podman`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			// Initialize runtime factory based on flag
			rt := types.RuntimeType(runtimeType)
			if !rt.Valid() {
				return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
			}

			vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
			logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

			// Check if podman runtime is being used on unsupported platform
			if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
				return err
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return info.Run(vars.RuntimeFactory.GetRuntimeType())
		},
	}

	// Add runtime flag as required
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")

	return cmd
}
