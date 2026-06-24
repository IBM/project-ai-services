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
	logger.Debugln("Validating sentient group exists")

	// Check if sentient group exists using getent
	cmd := exec.Command("getent", "group", constants.SentientGroupName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s group does not exist", constants.SentientGroupName)
	}

	logger.Debugf("✓ %s group exists", constants.SentientGroupName)

	return nil
}

func (r *UsergroupRule) Message() string {
	return fmt.Sprintf("%s group exists", constants.SentientGroupName)
}

func (r *UsergroupRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *UsergroupRule) Hint() string {
	return "The sentient group is required for ulimit configurations. Run 'ai-services bootstrap configure' to create the group."
}

// Made with Bob
