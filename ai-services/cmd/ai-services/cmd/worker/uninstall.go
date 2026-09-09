package worker

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := context.Background()
			runtimeType := vars.RuntimeFactory.GetRuntimeType()
			rt, err := runtime.CreateRuntime(runtimeType, "")
			if err != nil {
				return fmt.Errorf("worker uninstall: init runtime: %w", err)
			}
			isLocalWorker, err := cmdcommon.IsCatalogLocalWorker(ctx, rt)
			if err != nil {
				return fmt.Errorf("could not determine LOCAL_WORKER from catalog pod: %w", err)
			}
			if isLocalWorker {
				return fmt.Errorf("the worker is co-located with the control plane and cannot be uninstalled independently")
			}

			return workeruninstall.Uninstall(cmd.Context(), workerutils.UninstallOptions{
				RuntimeType: runtimeType,
				AutoYes:     uninstallAutoYes,
				SkipCleanup: skipCleanup,
			})
		},
	}

	cmdcommon.ConfigureRuntimeFlag(cmd, &uninstallRuntimeType)

	cmd.Flags().BoolVarP(&uninstallAutoYes, "yes", "y", false,
		"Automatically accept all confirmation prompts.")

	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false,
		"Skip deleting worker voulme (default=false)")

	return cmd
}
