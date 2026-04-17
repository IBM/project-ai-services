package podman

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/check"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
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
	if err := systemctl("start", "podman.socket"); err != nil {
		return fmt.Errorf("failed to start podman socket: %w", err)
	}
	// enable podman socket
	if err := systemctl("enable", "podman.socket"); err != nil {
		return fmt.Errorf("failed to enable podman socket: %w", err)
	}

	logger.Infoln("Waiting for podman socket to be ready...", logger.VerbosityLevelDebug)
	time.Sleep(podmanSocketWaitDuration) // wait for socket to be ready

	if err := validators.PodmanHealthCheck(); err != nil {
		return fmt.Errorf("podman health check failed after configuration: %w", err)
	}

	logger.Infof("Podman configured successfully.")

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
