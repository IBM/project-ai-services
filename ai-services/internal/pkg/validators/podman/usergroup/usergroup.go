package usergroup

import (
	"fmt"
	"os/exec"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type UsergroupRule struct{}

func NewUsergroupRule() *UsergroupRule {
	return &UsergroupRule{}
}

func (r *UsergroupRule) Name() string {
	return "usergroup"
}

func (r *UsergroupRule) Description() string {
	return "Validates that the sentient group exists for ulimit configurations."
}

func (r *UsergroupRule) Verify() error {
	logger.Infoln("Validating sentient group exists", logger.VerbosityLevelDebug)

	// Check if sentient group exists using getent
	cmd := exec.Command("getent", "group", "sentient")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sentient group does not exist")
	}

	logger.Infoln("✓ sentient group exists", logger.VerbosityLevelDebug)

	return nil
}

func (r *UsergroupRule) Message() string {
	return "Sentient group exists"
}

func (r *UsergroupRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *UsergroupRule) Hint() string {
	return "The sentient group is required for ulimit configurations. Run 'ai-services bootstrap configure' to create the group."
}

// Made with Bob
