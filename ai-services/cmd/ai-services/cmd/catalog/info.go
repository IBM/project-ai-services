package catalog

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/info"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// NewInfoCmd creates a new info command for the catalog service.
func NewInfoCmd() *cobra.Command {
	var (
		runtimeType string
	)
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

			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return info.Run(vars.RuntimeFactory.GetRuntimeType())
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}
