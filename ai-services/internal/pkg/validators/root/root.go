package root

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type RootRule struct{}

func NewRootRule() *RootRule {
	return &RootRule{}
}

func (r *RootRule) Name() string {
	return "root"
}

func (r *RootRule) Verify() error {
	euid := os.Geteuid()

	logger.Infoln("Checking root privileges", 2)

	if euid != 0 {
		return fmt.Errorf("current user is not root (EUID: %d)", euid)
	}

	return nil
}

func (r *RootRule) Message() string {
	return "Current user is root"
}
