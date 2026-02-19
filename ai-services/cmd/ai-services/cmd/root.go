package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Global runtime type flag.
	runtimeType string
)

// RootCmd represents the base command when called without any subcommands.
var RootCmd = &cobra.Command{
	Use:     "ai-services",
	Short:   "AI Services CLI",
	Long:    `A CLI tool for managing AI Services infrastructure.`,
	Version: version.GetVersion(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		// Ensures logs flush after each command run
		logger.Infoln("Logger initialized (PersistentPreRun)", logger.VerbosityLevelDebug)

		// Initialize runtime factory based on flag or environment
		rt := types.RuntimeType(runtimeType)
		if !rt.Valid() {
			return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift')", runtimeType)
		}

		vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
		logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	defer logger.Flush()
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	logger.Init()
	RootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	// Add runtime flag
	RootCmd.PersistentFlags().StringVar(
		&runtimeType,
		"runtime",
		string(types.RuntimeTypePodman),
		fmt.Sprintf("Container runtime to use (options: %s, %s).", types.RuntimeTypePodman, types.RuntimeTypeOpenShift),
	)

	RootCmd.AddCommand(version.VersionCmd)
	RootCmd.AddCommand(bootstrap.BootstrapCmd())
	RootCmd.AddCommand(application.ApplicationCmd)
}
