package worker

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workeruninstall "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

// Flag variables for the worker uninstall command.
var (
	uninstallRuntimeType string
	uninstallAutoYes     bool
	skipCleanup          bool
)

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove this node's worker components",
		Long: `Removes all worker components deployed by 'worker join' on this node.

The uninstall process will:
  - Delete the Caddy reverse-proxy pod
  - Remove the worker data directory (<basedir>/worker)

Application pods deployed on this worker by the catalog are not touched.`,
		Example: `  # Uninstall worker components (prompts for confirmation)
  ai-services worker uninstall --runtime podman

  # Skip confirmation prompt
  ai-services worker uninstall --runtime podman --yes

  ai-services worker uninstall --runtime podman --yes`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			return cmdcommon.InitAndValidateRuntimeFlag(uninstallRuntimeType)
		},
		RunE: uninstallRunE,
	}

	cmdcommon.ConfigureRuntimeFlag(cmd, &uninstallRuntimeType)

	cmd.Flags().BoolVarP(&uninstallAutoYes, "yes", "y", false,
		"Automatically accept all confirmation prompts.")

	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false,
		"Skip deleting worker voulme (default=false)")

	return cmd
}

func uninstallRunE(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()
	rtType := vars.RuntimeFactory.GetRuntimeType()

	rt, err := runtime.CreateRuntime(rtType, workerconstants.WorkerAppName)
	if err != nil {
		return fmt.Errorf("worker uninstall: init runtime: %w", err)
	}

	if err := checkNotLocalWorkerForRuntime(ctx, rtType, rt); err != nil {
		return err
	}

	return workeruninstall.Uninstall(ctx, workerutils.UninstallOptions{
		RuntimeType: rtType,
		AutoYes:     uninstallAutoYes,
		SkipCleanup: skipCleanup,
	})
}

func checkNotLocalWorkerForRuntime(ctx context.Context, rtType types.RuntimeType, rt runtime.Runtime) error {
	var (
		localWorker bool
		err         error
	)

	switch rtType {
	case types.RuntimeTypeOpenShift:
		localWorker, err = workercommon.IsOpenShiftLocalWorker(ctx, rt)
	default:
		localWorker, err = workercommon.IsPodmanLocalWorker(ctx, rt)
	}

	if err != nil {
		return fmt.Errorf("could not determine LOCAL_WORKER from catalog pod: %w", err)
	}
	if localWorker {
		return fmt.Errorf("the worker is co-located with the control plane and cannot be uninstalled independently")
	}

	return nil
}
