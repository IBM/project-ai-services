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
	runtimeType     string
	outputDir       string
	applicationName string
	namespace       string
)

// gatherOptions carries options forwarded from the cobra command.
type gatherOptions struct {
	outputDir       string
	applicationName string
	namespace       string
}

// gatherer is the common interface implemented by every runtime-specific collector.
type gatherer interface {
	gather(opts gatherOptions) (string, error)
}

// MustGatherCmd returns the must-gather cobra command.
func MustGatherCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "must-gather",
		Short: "Collect debugging information from an AI Services deployment",
		Long: `Collects comprehensive debugging information from an AI Services deployment
for support and troubleshooting purposes.

Gathered data includes pod details, container logs, network and volume
information. All sensitive values are automatically redacted.`,
		Example: `  # Collect from all applications (podman)
  ai-services must-gather --runtime podman

  # Collect from a specific application (podman)
  ai-services must-gather --runtime podman --application rag

  # Write output to a custom directory
  ai-services must-gather --runtime podman --output-dir /tmp/debug`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: mustGatherPreRun,
		RunE:              mustGatherRun,
	}

	cmd.PersistentFlags().StringVar(&runtimeType, "runtime", "",
		fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkPersistentFlagRequired("runtime")

	cmd.PersistentFlags().StringVarP(&outputDir, "output-dir", "o", ".",
		"Base directory for output (a must-gather.local.<id> sub-directory is created inside)")

	cmd.PersistentFlags().StringVarP(&applicationName, "application", "a", "",
		"Limit collection to this application name (default: all applications)")

	return cmd
}

func mustGatherPreRun(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf(
			"invalid runtime type: %s (must be 'podman' or 'openshift'). "+
				"Please specify runtime using --runtime flag", runtimeType,
		)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Debugf("Using runtime: %s\n", rt)

	return nil
}

func mustGatherRun(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	opts := gatherOptions{
		outputDir:       outputDir,
		applicationName: applicationName,
		namespace:       namespace,
	}

	g, err := newGatherer(vars.RuntimeFactory.GetRuntimeType())
	if err != nil {
		return err
	}

	outDir, err := g.gather(opts)
	if err != nil {
		return fmt.Errorf("must-gather failed: %w", err)
	}

	logger.Infof("Must-gather complete. Output saved to: %s\n", outDir)

	return nil
}

// newGatherer returns the gatherer implementation for the given runtime type.
func newGatherer(rt types.RuntimeType) (gatherer, error) {
	switch rt {
	case types.RuntimeTypePodman:
		return newPodmanGatherer(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime for must-gather: %s", rt)
	}
}
