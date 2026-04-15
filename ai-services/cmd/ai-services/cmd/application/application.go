package application

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/image"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/model"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	hiddenTemplates bool
	// Runtime type flag for application command.
	runtimeType string
)

// ApplicationCmd represents the application command.
var ApplicationCmd = &cobra.Command{
	Use:   "application",
	Short: "Deploy and monitor the applications",
	Long:  `The application command helps you deploy and monitor the applications`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		// Initialize runtime factory based on flag
		rt := types.RuntimeType(runtimeType)
		if !rt.Valid() {
			return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
		}

		vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
		logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

		return nil
	},
}

func init() {
	ApplicationCmd.AddCommand(templatesCmd)
	ApplicationCmd.AddCommand(createCmd)
	ApplicationCmd.AddCommand(psCmd)
	ApplicationCmd.AddCommand(deleteCmd)
	ApplicationCmd.AddCommand(image.ImageCmd)
	ApplicationCmd.AddCommand(stopCmd)
	ApplicationCmd.AddCommand(startCmd)
	ApplicationCmd.AddCommand(infoCmd)
	ApplicationCmd.AddCommand(logsCmd)
	ApplicationCmd.AddCommand(model.ModelCmd)

	// Add runtime flag as required
	ApplicationCmd.PersistentFlags().StringVar(&runtimeType, "runtime", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = ApplicationCmd.MarkPersistentFlagRequired("runtime")

	ApplicationCmd.PersistentFlags().StringVar(&vars.ToolImage, "tool-image", vars.ToolImage, "Tool image to use for downloading the model(only for the development purpose)")
	ApplicationCmd.PersistentFlags().BoolVar(&hiddenTemplates, "hidden", false, "Show hidden templates")
	_ = ApplicationCmd.PersistentFlags().MarkHidden("tool-image")
	_ = ApplicationCmd.PersistentFlags().MarkHidden("hidden")
}
