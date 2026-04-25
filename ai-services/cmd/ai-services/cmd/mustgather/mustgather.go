package mustgather

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Runtime type flag for must-gather command.
	runtimeType string
	// Output directory for collected data.
	outputDir string
	// Application name to gather data for.
	applicationName string
)

// MustGatherCmd represents the must-gather command.
func MustGatherCmd() *cobra.Command {
	mustGatherCmd := &cobra.Command{
		Use:   "must-gather",
		Short: "Collect debugging information from the system",
		Long: `The must-gather command collects comprehensive debugging information from the system
including pod details, logs, events, and configurations. Secrets are automatically sanitized.`,
		Example: mustGatherExample(),
		Args:    cobra.NoArgs,
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
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			rt := vars.RuntimeFactory.GetRuntimeType()

			// Create must-gather instance based on runtime
			factory := NewMustGatherFactory(rt)
			gatherer, err := factory.Create()
			if err != nil {
				return fmt.Errorf("failed to create must-gather instance: %w", err)
			}

			opts := MustGatherOptions{
				OutputDir:       outputDir,
				ApplicationName: applicationName,
			}

			if err := gatherer.Gather(opts); err != nil {
				return fmt.Errorf("failed to gather debugging information: %w", err)
			}

			logger.Infoln("Must-gather completed successfully")
			logger.Infof("Debugging information saved to: %s\n", outputDir)

			return nil
		},
	}

	// Add runtime flag as required
	mustGatherCmd.PersistentFlags().StringVar(&runtimeType, "runtime", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = mustGatherCmd.MarkPersistentFlagRequired("runtime")

	// Add output directory flag
	mustGatherCmd.PersistentFlags().StringVarP(&outputDir, "output-dir", "o", ".", "Base directory to save collected debugging information (creates must-gather.local.<id> subdirectory)")

	// Add application name flag (optional)
	mustGatherCmd.PersistentFlags().StringVarP(&applicationName, "application", "a", "", "Specific application name to gather data for (optional, gathers all if not specified)")

	return mustGatherCmd
}

func mustGatherExample() string {
	return `  # Collect debugging information for podman runtime
  ai-services must-gather --runtime podman

  # Collect debugging information for openshift runtime
  ai-services must-gather --runtime openshift

  # Collect debugging information for a specific application
  ai-services must-gather --runtime podman --application rag

  # Specify custom output directory
  ai-services must-gather --runtime openshift --output-dir /tmp/debug-info`
}

// Made with Bob
