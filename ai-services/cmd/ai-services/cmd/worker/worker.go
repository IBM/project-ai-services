// Package worker provides CLI commands that run on a worker node.
// These commands are distinct from the catalog worker management commands
// (catalog worker register/list/deregister) which target the catalog API.
package worker

import "github.com/spf13/cobra"

// WorkerCmd returns the top-level worker command.
func WorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage this node as a worker",
		Long: `Commands that run on a worker node to connect it to the catalog control plane.

To join this node as a worker:
  1. On the catalog node, register the worker:
       ai-services catalog worker register <name>
  2. On this node, run join with the printed token:
       ai-services worker join <catalog-host>:9090 --token <token>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newJoinCmd())
	cmd.AddCommand(newUninstallCmd())
	cmd.AddCommand(newGrpcServerCmd())

	return cmd
}
