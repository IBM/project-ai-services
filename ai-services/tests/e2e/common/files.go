package common

import (
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const dirPerm = 0o755 // standard permission for directories

// CreateDir creates a directory if it does not exist.
func CreateDir(path string) {
	if err := os.MkdirAll(path, dirPerm); err != nil {
		logger.Errorln("Failed to create directory: " + path + " : " + err.Error())

		return
	}

	logger.Infoln("Directory ensured: "+path, logger.VerbosityLevelDebug)
}
