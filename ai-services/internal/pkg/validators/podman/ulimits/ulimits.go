package ulimits

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type UlimitsRule struct{}

func NewUlimitsRule() *UlimitsRule {
	return &UlimitsRule{}
}

func (r *UlimitsRule) Name() string {
	return "ulimits"
}

func (r *UlimitsRule) Description() string {
	return "Validates that memlock and nofile ulimits are properly configured for the sentient group."
}

func (r *UlimitsRule) Verify() error {
	logger.Debugln("Validating ulimit configurations")

	// Validate memlock configuration
	if err := r.validateMemlockConf(); err != nil {
		return err
	}

	// Validate nofile configuration
	if err := r.validateNofileConf(); err != nil {
		return err
	}

	logger.Debugln("✓ ulimit configurations are valid")

	return nil
}

func (r *UlimitsRule) validateMemlockConf() error {
	lines, err := utils.ReadFileLines(constants.MemlockConfFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("memlock configuration file does not exist: %s", constants.MemlockConfFile)
		}

		return fmt.Errorf("failed to read memlock configuration: %w", err)
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == constants.ExpectedMemlockConfig {
			logger.Debugln("✓ memlock configuration is valid")

			return nil
		}
	}

	return fmt.Errorf("memlock configuration not found or invalid in %s", constants.MemlockConfFile)
}

func (r *UlimitsRule) validateNofileConf() error {
	lines, err := utils.ReadFileLines(constants.NofileConfFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("nofile configuration file does not exist: %s", constants.NofileConfFile)
		}

		return fmt.Errorf("failed to read nofile configuration: %w", err)
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for @sentient hard nofile configuration
		if err := r.checkNofileLine(line); err == nil {
			logger.Debugln("✓ nofile configuration is valid")

			return nil
		} else if err.Error() != "not a nofile line" {
			return err
		}
	}

	return fmt.Errorf("nofile configuration not found or invalid in %s", constants.NofileConfFile)
}

// checkNofileLine validates a single nofile configuration line.
func (r *UlimitsRule) checkNofileLine(line string) error {
	sentientPrefix := "@" + constants.SentientGroupName
	if !strings.HasPrefix(line, sentientPrefix) || !strings.Contains(line, "nofile") {
		return fmt.Errorf("not a nofile line")
	}

	parts := strings.Fields(line)
	if len(parts) < constants.NofileFieldCount || parts[1] != "hard" || parts[2] != "nofile" {
		return fmt.Errorf("not a nofile line")
	}

	value, err := strconv.Atoi(parts[3])
	if err != nil {
		return fmt.Errorf("not a nofile line")
	}

	if value < constants.MinNofileLimit {
		return fmt.Errorf("nofile limit (%d) is below minimum required (%d)", value, constants.MinNofileLimit)
	}

	return nil
}

func (r *UlimitsRule) Message() string {
	return "Ulimits (memlock and nofile) are properly configured"
}

func (r *UlimitsRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *UlimitsRule) Hint() string {
	return "Ulimit configurations are required for pods to run properly. Run 'ai-services bootstrap configure' to set up the required ulimits."
}

// Made with Bob
