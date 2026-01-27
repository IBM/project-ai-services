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

	logger.Infoln("Checking root privileges", logger.VerbosityLevelDebug)

	if euid != 0 {
		return fmt.Errorf("current user is not root (EUID: %d)", euid)
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
