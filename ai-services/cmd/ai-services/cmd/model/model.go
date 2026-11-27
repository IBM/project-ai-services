// Package model provides CLI commands to manage models registered with the catalog.
package model

import (
	"fmt"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
)

var runtimeType string

// ModelCmd returns the top-level model command.
func ModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage models",
		Long:  `Manage the models (local and remote) registered with the catalog.`,
		Args:  cobra.MaximumNArgs(0),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if runtimeType == "" {
				return fmt.Errorf("required flag(s) \"runtime\" not set")
			}

			return cmdcommon.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmdcommon.ConfigurePersistentRuntimeFlag(cmd, &runtimeType)

	cmd.AddCommand(newInfoCmd())

	return cmd
}
