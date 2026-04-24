package podman

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/check"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// dirPermissions is the default permission for creating directories.
	dirPermissions = 0755
)

// configureSpyre validates and repairs Spyre card configuration.
func configureSpyre() error {
	logger.Infoln("Running Spyre configuration validation and repair...", logger.VerbosityLevelDebug)

	// Check if Spyre cards are present
	if !spyre.IsApplicable() {
		logger.Infoln("No Spyre cards detected. Validation not applicable.", logger.VerbosityLevelDebug)

		return nil
	}

	numCards := spyre.GetNumberOfSpyreCards()
	logger.Infof("Detected %d Spyre card(s)", numCards)

	// Run validation and repair
	allPassed := runValidationAndRepair()

	// Add current user to sentient group
	if err := configureUsergroup(); err != nil {
		return err
	}

	if !allPassed {
		return fmt.Errorf("some Spyre configuration checks still failed after repair")
	}

	logger.Infoln("✓ All Spyre configuration checks passed", logger.VerbosityLevelDebug)

	return nil
}

// runValidationAndRepair runs validation checks and attempts repairs if needed.
func runValidationAndRepair() bool {
	// Run all validation checks
	checks := spyre.RunChecks()

	// Check if any validation failed
	allPassed := checkValidationResults(checks)

	// If checks failed, attempt repairs
	if !allPassed {
		allPassed = attemptRepairs(checks)
	}

	return allPassed
}

// checkValidationResults checks if all validation checks passed.
func checkValidationResults(checks []check.CheckResult) bool {
	allPassed := true
	for _, check := range checks {
		if !check.GetStatus() {
			allPassed = false
			logger.Infof("Check failed: %s", check.String())
		}
	}

	return allPassed
}

// attemptRepairs attempts to repair failed checks and re-validates.
func attemptRepairs(checks []check.CheckResult) bool {
	logger.Infoln("Attempting automatic repairs...", logger.VerbosityLevelDebug)
	results := spyre.Repair(checks)

	logRepairResults(results)

	// Re-run checks after repair
	logger.Infoln("Re-running validation...", logger.VerbosityLevelDebug)
	checks = spyre.RunChecks()

	allPassed := true
	for _, check := range checks {
		if !check.GetStatus() {
			allPassed = false
		}
	}

	return allPassed
}

// logRepairResults logs the results of repair operations.
func logRepairResults(results []spyre.RepairResult) {
	for _, result := range results {
		switch result.Status {
		case spyre.StatusFixed:
			logger.Infof("✓ Fixed: %s", result.CheckName)
		case spyre.StatusFailedToFix:
			logger.Infof("✗ Failed to fix: %s - %v", result.CheckName, result.Error)
		case spyre.StatusNotFixable:
			logger.Infof("⚠ Not fixable: %s - %s", result.CheckName, result.Message)
		case spyre.StatusSkipped:
			// Skip logging for skipped checks
		default:
			logger.Infof("Unknown status for %s: %s", result.CheckName, result.Status)
		}
	}
}

func configureUsergroup() error {
	cmd_str := `usermod -aG sentient $USER`
	cmd := exec.Command("bash", "-c", cmd_str)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create sentient group and add current user to the sentient group. Error: %w, output: %s", err, string(out))
	}

	return nil
}

func installPodman() error {
	cmd := exec.Command("dnf", "-y", "install", "podman")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install podman: %v, output: %s", err, string(out))
	}

	return nil
}

func setupPodman() error {
	// start podman socket
	if err := systemctl("restart", "podman.socket"); err != nil {
		return fmt.Errorf("failed to start podman socket: %w", err)
	}
	// enable podman socket
	if err := systemctl("enable", "podman.socket"); err != nil {
		return fmt.Errorf("failed to enable podman socket: %w", err)
	}

	logger.Infoln("Waiting for podman socket to be ready...", logger.VerbosityLevelDebug)
	time.Sleep(podmanSocketWaitDuration) // wait for socket to be ready

	if err := utils.PodmanHealthCheck(); err != nil {
		return fmt.Errorf("podman health check failed after configuration: %w", err)
	}

	logger.Infof("Podman configured successfully.")

	return nil
}

func configurePodmanGroups() error {
	logger.Infoln("Configuring podman service supplementary groups...", logger.VerbosityLevelDebug)

	// Check if Spyre cards are present - only needed if Spyre cards exist
	if !spyre.IsApplicable() {
		logger.Infoln("No Spyre cards detected. Skipping podman service supplementary groups configuration.", logger.VerbosityLevelDebug)

		return nil
	}

	// Run the check
	checkResult := spyre.CheckPodmanServiceSupplementaryGroups()
	if checkResult.GetStatus() {
		logger.Infoln("✓ Podman service supplementary groups already configured", logger.VerbosityLevelDebug)

		return nil
	}

	// Attempt repair
	logger.Infoln("Fixing podman service supplementary groups configuration...", logger.VerbosityLevelDebug)
	if err := fixPodmanServiceSupplementaryGroups(); err != nil {
		return fmt.Errorf("failed to configure podman service supplementary groups: %w", err)
	}

	logger.Infof("✓ Podman service supplementary groups configured successfully")

	return nil
}

// fixPodmanServiceSupplementaryGroups repairs the podman service SupplementaryGroups configuration.
//
// This function addresses the issue where Podman operations invoked via the socket (e.g., through
// systemd or remote API calls) lack access to VFIO devices because the service doesn't inherit
// the user's supplementary groups. While shell-based Podman commands work fine (inheriting the
// user's 'sentient' group), socket-based operations fail without explicit configuration.
//
// The repair process:
//  1. Creates a systemd drop-in file at /etc/systemd/system/podman.service.d/override.conf
//     containing: [Service]\nSupplementaryGroups=sentient
//  2. Reloads the systemd daemon to pick up the new configuration
//  3. Restarts both podman.service and podman.socket to apply the changes
//
// This ensures that all Podman operations, regardless of invocation method, have the necessary
// permissions to access VFIO devices (/dev/vfio/*) required for Spyre card functionality.
func fixPodmanServiceSupplementaryGroups() error {
	if err := createPodmanServiceDropIn(); err != nil {
		return err
	}

	if err := reloadAndRestartPodmanServices(); err != nil {
		return err
	}

	return nil
}

func createPodmanServiceDropIn() error {
	dropInDir := "/etc/systemd/system/podman.service.d"
	if err := os.MkdirAll(dropInDir, dirPermissions); err != nil {
		return err
	}

	dropInFile := dropInDir + "/override.conf"
	dropInContent := `[Service]
SupplementaryGroups=sentient
`

	return os.WriteFile(dropInFile, []byte(dropInContent), utils.FilePermissions)
}

func reloadAndRestartPodmanServices() error {
	// Reload systemd daemon
	cmd := exec.Command("systemctl", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %v, output: %s", err, string(out))
	}

	// Restart podman service
	cmd = exec.Command("systemctl", "restart", "podman.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart podman.service: %v, output: %s", err, string(out))
	}

	// Restart podman socket
	cmd = exec.Command("systemctl", "restart", "podman.socket")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart podman.socket: %v, output: %s", err, string(out))
	}

	return nil
}

func systemctl(action, unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", action, unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to %s %s: %v, output: %s", action, unit, err, string(out))
	}

	return nil
}

func setupSMTLevel() error {
	// Check current SMT level first
	cmd := exec.Command("ppc64_cpu", "--smt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check current SMT level: %v, output: %s", err, string(out))
	}

	currentSMTLevel, err := getSMTLevel(string(out))
	if err != nil {
		return fmt.Errorf("failed to get current SMT level: %w", err)
	}

	logger.Infof("Current SMT level is %d", currentSMTLevel, logger.VerbosityLevelDebug)

	// 1. Enable smtstate.service
	if err := systemctl("enable", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to enable smtstate.service: %w", err)
	}
	logger.Infoln("smtstate.service enabled successfully", logger.VerbosityLevelDebug)

	// 2. Start smtstate.service
	if err := systemctl("start", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to start smtstate.service: %w", err)
	}
	logger.Infoln("smtstate.service started successfully", logger.VerbosityLevelDebug)

	// 3. Set SMT level to 2
	if currentSMTLevel != constants.SMTLevel {
		logger.Infof("Setting SMT level from %d to %d", currentSMTLevel, constants.SMTLevel, logger.VerbosityLevelDebug)
		cmd = exec.Command("ppc64_cpu", fmt.Sprintf("--smt=%d", constants.SMTLevel))
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to set SMT level to %d: %v, output: %s", constants.SMTLevel, err, string(out))
		}
		logger.Infof("SMT level set to %d", constants.SMTLevel, logger.VerbosityLevelDebug)
	} else {
		logger.Infof("SMT level is already set to %d", constants.SMTLevel, logger.VerbosityLevelDebug)
	}

	// 4. Restart smtstate.service to persist the setting
	if err := systemctl("restart", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to restart smtstate.service: %w", err)
	}
	logger.Infoln("smtstate.service restarted successfully", logger.VerbosityLevelDebug)

	// 5. Verify the SMT level is set correctly
	cmd = exec.Command("ppc64_cpu", "--smt")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check current SMT level: %v, output: %s", err, string(out))
	}

	smtLevel, err := getSMTLevel(string(out))
	if err != nil {
		return fmt.Errorf("failed to get current SMT level: %w", err)
	}
	logger.Infof("SMT level verified: %d", smtLevel, logger.VerbosityLevelDebug)

	return nil
}

func getSMTLevel(output string) (int, error) {
	out := strings.TrimSpace(output)

	if !strings.HasPrefix(out, "SMT=") {
		return 0, fmt.Errorf("unexpected output: %s", out)
	}

	SMTLevelStr := strings.TrimPrefix(out, "SMT=")
	SMTlevel, err := strconv.Atoi(SMTLevelStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse SMT level: %w", err)
	}

	return SMTlevel, nil
}
