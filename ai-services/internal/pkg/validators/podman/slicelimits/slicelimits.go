package slicelimits

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type SliceLimitsRule struct{}

func NewSliceLimitsRule() *SliceLimitsRule {
	return &SliceLimitsRule{}
}

func (r *SliceLimitsRule) Name() string {
	return "slicelimits"
}

func (r *SliceLimitsRule) Description() string {
	return "Validates that systemd user slice limits are configured for rootless podman."
}

func (r *SliceLimitsRule) Verify() error {
	logger.Debugln("Validating systemd user slice limits")

	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		// Not running via sudo, skip this check
		logger.Debugln("Not running via sudo, skipping systemd slice limits check")

		return nil
	}

	userID, err := r.getUserID(sudoUser)
	if err != nil {
		return fmt.Errorf("failed to get user ID for %s: %w", sudoUser, err)
	}

	// Skip if user is root (UID 0)
	if userID == "0" {
		logger.Debugln("User is root, skipping systemd slice limits check")

		return nil
	}

	limitsFile := fmt.Sprintf("/etc/systemd/system/user-%s.slice.d/limits.conf", userID)

	if err := r.validateLimitsFile(limitsFile); err != nil {
		return err
	}

	logger.Debugln("✓ systemd user slice limits are valid")

	return nil
}

func (r *SliceLimitsRule) getUserID(username string) (string, error) {
	cmd := exec.Command("id", "-u", username)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get user ID: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func (r *SliceLimitsRule) validateLimitsFile(limitsFile string) error {
	lines, err := utils.ReadFileLines(limitsFile)
	if err != nil {
		return r.handleReadError(err, limitsFile)
	}

	hasNofile, hasMemlock := r.checkLimits(lines)

	return r.validateLimits(hasNofile, hasMemlock, limitsFile)
}

func (r *SliceLimitsRule) handleReadError(err error, limitsFile string) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("systemd slice limits file does not exist: %s", limitsFile)
	}

	return fmt.Errorf("failed to read systemd slice limits file: %w", err)
}

func (r *SliceLimitsRule) checkLimits(lines []string) (bool, bool) {
	hasNofile := false
	hasMemlock := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if after, ok := strings.CutPrefix(line, "LimitNOFILE="); ok {
			if value, err := strconv.Atoi(after); err == nil && value >= constants.MinNofileLimit {
				hasNofile = true
			}
		}

		if after, ok := strings.CutPrefix(line, "LimitMEMLOCK="); ok {
			if after == "infinity" {
				hasMemlock = true
			}
		}
	}

	return hasNofile, hasMemlock
}

func (r *SliceLimitsRule) validateLimits(hasNofile, hasMemlock bool, limitsFile string) error {
	if !hasNofile {
		return fmt.Errorf("LimitNOFILE not configured or below minimum (%d) in %s", constants.MinNofileLimit, limitsFile)
	}

	if !hasMemlock {
		return fmt.Errorf("LimitMEMLOCK not configured to infinity in %s", limitsFile)
	}

	return nil
}

func (r *SliceLimitsRule) Message() string {
	return "Systemd user slice limits are properly configured"
}

func (r *SliceLimitsRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelWarning
}

func (r *SliceLimitsRule) Hint() string {
	return "Systemd user slice limits are required for rootless podman to have proper ulimits. Run 'ai-services bootstrap configure' to set up the required limits."
}

// Made with Bob
