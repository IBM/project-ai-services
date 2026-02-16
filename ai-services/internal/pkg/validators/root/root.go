package root

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type RootRule struct{}

func NewRootRule() *RootRule {
	return &RootRule{}
}

func (r *RootRule) Name() string {
	return "root"
}

func (r *RootRule) Description() string {
	return "Validates that the current user has root privileges."
}

func (r *RootRule) Verify() error {
	euid := os.Geteuid()
	if euid == 0 {
		logger.Infoln("running command as root", logger.VerbosityLevelDebug)

		return nil
	}

	if euid != 0 && os.Getenv("XDG_RUNTIME_DIR") == "" {
		uid := os.Getuid()
		logger.Infoln("running command as %s", uid, logger.VerbosityLevelDebug)
		if err := os.Setenv("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", uid)); err != nil {
			return fmt.Errorf("failed to set XDG_RUNTIME_DIR: %w", err)
		}
	}

	return nil
}

func (r *RootRule) Message() string {
	return "Current user is root"
}

func (r *RootRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *RootRule) Hint() string {
	return "Run this command with root privileges using 'sudo' or as the root user"
}
