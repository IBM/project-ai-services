package catalog

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewWorkerCmd returns the parent command for worker management.
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage workers registered with the catalog",
		Long: `Register, list, and deregister remote worker nodes.

Workers connect back to the catalog gRPC gateway using the bootstrap token
that is printed by the 'register' subcommand.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newWorkerRegisterCmd())
	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerDeregisterCmd())

	return cmd
}

// ─── register ────────────────────────────────────────────────────────────────

func newWorkerRegisterCmd() *cobra.Command {
	var runtimeType string

	cmd := &cobra.Command{
		Use:   "register <name>",
		Short: "Pre-register a worker and obtain its bootstrap token",
		Long: `Pre-registers a worker by name in the catalog and returns a single-use
bootstrap token.

Pass the token to the worker daemon at startup so it can authenticate with the
catalog gRPC gateway:

  worker start --token <token> --gateway <catalog-host>:9090`,
		Example: `  ai-services catalog worker register node-1 --runtime podman`,
		Args:    cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			c, err := client.New()
			if err != nil {
				return err
			}

			resp, err := c.CreateWorker(args[0])
			if err != nil {
				return err
			}

			logger.Infoln("Worker registered successfully.")
			logger.Infof("  Name:  %s\n", resp.WorkerName)
			logger.Infof("  Token: %s\n", resp.Token)
			logger.Infoln("\nPass this token to the worker daemon with --token.")
			logger.Infoln("The token is single-use and expires after 24 hours.")

			return nil
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func newWorkerListCmd() *cobra.Command {
	var runtimeType string

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all registered workers",
		Example: `  ai-services catalog worker list --runtime podman`,
		Args:    cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			c, err := client.New()
			if err != nil {
				return err
			}

			workers, err := c.ListWorkers()
			if err != nil {
				return err
			}

			return printWorkerTable(workers)
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}

// ─── deregister ───────────────────────────────────────────────────────────────

func newWorkerDeregisterCmd() *cobra.Command {
	var runtimeType string

	cmd := &cobra.Command{
		Use:   "deregister <id>",
		Short: "Permanently deregister a worker",
		Long: `Permanently removes a worker from the catalog by its UUID.

If the worker is currently connected its gRPC stream is also cleaned up.
Use 'ai-services catalog worker list' to find the worker's ID.`,
		Example: `  ai-services catalog worker deregister 4b3e1f2a-8c7d-4e5b-9f6a-1d2e3f4a5b6c --runtime podman`,
		Args:    cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			c, err := client.New()
			if err != nil {
				return err
			}

			if err := c.DeleteWorker(args[0]); err != nil {
				return err
			}

			logger.Infof("Worker %s deregistered.\n", args[0])

			return nil
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}

// ─── helpers ──────────────────────────────────────────────────────────────────

const workerTablePadding = 3

// printWorkerTable writes a tab-aligned worker list to stdout.
func printWorkerTable(workers []catalogtypes.Worker) error {
	if len(workers) == 0 {
		logger.Infoln("No workers registered.")

		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, workerTablePadding, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tNAME\tRUNTIME\tSTATUS\tLAST HEARTBEAT"); err != nil {
		return err
	}

	for _, worker := range workers {
		hb := "-"
		if worker.LastHeartbeat != nil {
			hb = worker.LastHeartbeat.UTC().Format(time.RFC3339)
		}

		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			worker.ID, worker.Name, worker.RuntimeType, worker.Status, hb); err != nil {
			return err
		}
	}

	return w.Flush()
}
