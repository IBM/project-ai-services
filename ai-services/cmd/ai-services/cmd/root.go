package cmd

import (
	"flag"
	"os"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// RootCmd represents the base command when called without any subcommands.
var RootCmd = &cobra.Command{
	Use:     "ai-services",
	Short:   "AI Services CLI",
	Long:    `A CLI tool for managing AI Services infrastructure.`,
	Version: version.GetVersion(),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Ensures logs flush after each command run
		logger.Infoln("Logger initialized (PersistentPreRun)", logger.VerbosityLevelDebug)
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

	RootCmd.AddCommand(version.VersionCmd)
	RootCmd.AddCommand(bootstrap.BootstrapCmd())
	RootCmd.AddCommand(application.ApplicationCmd)
}
