package version

import (
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/spf13/cobra"
)

var (
	// Below values will be overridden during build
	Version   string = "unknown"
	GitCommit string = "unknown"
	BuildDate string = ""
)

func GetVersion() string {
	return Version
}

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Prints CLI version with more info",
	Run: func(cmd *cobra.Command, args []string) {
		logger.Infof("Version: %s\nGitCommit: %s\nBuildDate: %s\n", Version, GitCommit, BuildDate)
	},
}
