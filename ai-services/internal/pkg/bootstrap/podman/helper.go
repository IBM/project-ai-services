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
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/selinux"
)

// configureSpyre validates and repairs Spyre card configuration.
func configureSpyre() error {
	logger.Debugln("Running Spyre configuration validation and repair...")

	// Check if Spyre cards are present
	if !spyre.IsApplicable() {
		logger.Debugln("No Spyre cards detected. Validation not applicable.")

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

	logger.Debugln("✓ All Spyre configuration checks passed")

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
	logger.Debugln("Attempting automatic repairs...")
	results := spyre.Repair(checks)

	logRepairResults(results)

	// Re-run checks after repair
	logger.Debugln("Re-running validation...")
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
	username := os.Getenv("SUDO_USER")
	if username == "" {
		// Fallback to current user if not running via sudo
		username = os.Getenv("USER")
		if username == "" {
			username = os.Getenv("LOGNAME")
		}
	}
	if username == "" {
		return fmt.Errorf("failed to determine current username: SUDO_USER, USER and LOGNAME environment variables are not set")
	}

	cmd_str := fmt.Sprintf("usermod -aG sentient %s", username)
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
	euid := os.Geteuid()
	sudoUser := os.Getenv("SUDO_USER")

	// Enable podman services based on user context
	if err := enablePodmanServices(euid == 0 && sudoUser == "", sudoUser); err != nil {
		return err
	}

	logger.Debugln("Waiting for podman socket to be ready...")
	time.Sleep(podmanSocketWaitDuration) // wait for socket to be ready

	if err := utils.PodmanHealthCheck(); err != nil {
		return fmt.Errorf("podman health check failed after configuration: %w", err)
	}

	return nil
}

// enablePodmanServices enables podman services for root or user context.
func enablePodmanServices(isRoot bool, sudoUser string) error {
	services := []string{"podman.socket", "podman-restart.service"}
	var args []string

	if isRoot {
		args = []string{"--now"}
	} else {
		args = []string{"--now", fmt.Sprintf("--machine=%s@.host", sudoUser), "--user"}
	}

	for _, svc := range services {
		if err := systemctl("enable", svc, args...); err != nil {
			return fmt.Errorf("failed to enable %s: %w", svc, err)
		}
	}

	return nil
}

func systemctl(action, unit string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	cmdArgs := make([]string, 0, len(args))
	cmdArgs = append(cmdArgs, action)
	cmdArgs = append(cmdArgs, args...)
	cmdArgs = append(cmdArgs, unit)

	cmd := exec.CommandContext(ctx, "systemctl", cmdArgs...)
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

	logger.Debugf("Current SMT level is %d", currentSMTLevel)

	// 1. Enable smtstate.service
	if err := systemctl("enable", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to enable smtstate.service: %w", err)
	}
	logger.Debugln("smtstate.service enabled successfully")

	// 2. Start smtstate.service
	if err := systemctl("start", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to start smtstate.service: %w", err)
	}
	logger.Debugln("smtstate.service started successfully")

	// 3. Set SMT level to 2
	if currentSMTLevel != constants.SMTLevel {
		logger.Debugf("Setting SMT level from %d to %d", currentSMTLevel, constants.SMTLevel)
		cmd = exec.Command("ppc64_cpu", fmt.Sprintf("--smt=%d", constants.SMTLevel))
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to set SMT level to %d: %v, output: %s", constants.SMTLevel, err, string(out))
		}
		logger.Debugf("SMT level set to %d", constants.SMTLevel)
	} else {
		logger.Debugf("SMT level is already set to %d", constants.SMTLevel)
	}

	// 4. Restart smtstate.service to persist the setting
	if err := systemctl("restart", "smtstate.service"); err != nil {
		return fmt.Errorf("failed to restart smtstate.service: %w", err)
	}
	logger.Debugln("smtstate.service restarted successfully")

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
	logger.Debugf("SMT level verified: %d", smtLevel)

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

// setupSELinuxPodmanSocketPolicy configures SELinux policy for Podman socket access.
func setupSELinuxPodmanSocketPolicy() error {
	// Apply root Podman socket policy
	result := spyre.ApplySELinuxPolicy(
		"SELinux Podman socket policy configuration",
		"ai_services_root_policy",
		selinux.RootPodmanSocketPolicyContent,
		"SELinux Podman socket policy configured successfully",
	)

	if result.Status == spyre.StatusFailedToFix {
		return result.Error
	}

	// Apply rootless Podman socket policy
	rootlessResult := spyre.ApplySELinuxPolicy(
		"SELinux rootless Podman socket policy configuration",
		"ai_services_nonroot_policy",
		selinux.RootlessPodmanSocketPolicyContent,
		"SELinux rootless Podman socket policy configured successfully",
	)

	if rootlessResult.Status == spyre.StatusFailedToFix {
		return rootlessResult.Error
	}

	return nil
}

// ensurePodmanInstalled checks if podman is installed and installs it if needed.
func ensurePodmanInstalled(ctx context.Context) error {
	s := spinner.New("Checking podman installation")
	s.Start(ctx)

	if _, err := utils.Podman(); err != nil {
		s.UpdateMessage("Installing podman")
		if err := installPodman(); err != nil {
			s.Fail("failed to install podman")

			return err
		}
		s.Stop("podman installed successfully")
	} else {
		s.Stop("podman already installed")
	}

	return nil
}

// configurePodman enable podman services in the system.
func configurePodman(ctx context.Context) error {
	s := spinner.New("Configure podman")
	s.Start(ctx)

	if err := setupPodman(); err != nil {
		s.Fail("failed to configure podman")

		return err
	}
	s.Stop("Podman configured successfully")

	return nil
}

// ensureSpyreConfigured validates and repairs Spyre card configuration.
func ensureSpyreConfigured(ctx context.Context) error {
	s := spinner.New("Checking spyre card configuration")
	s.Start(ctx)

	if err := configureSpyre(); err != nil {
		s.Fail("failed to configure spyre card")

		return err
	}
	s.Stop("Spyre cards configuration validated successfully.")

	return nil
}

// ensureSMTConfigured sets up SMT level to 2 and persists via systemd.
func ensureSMTConfigured(ctx context.Context) error {
	s := spinner.New("Configuring SMT level to 2")
	s.Start(ctx)

	if err := setupSMTLevel(); err != nil {
		s.Fail("failed to configure SMT level")

		return err
	}
	s.Stop("SMT level configured successfully (set to 2)")

	return nil
}

// ensureSELinuxPolicyConfigured sets up SELinux policy for Podman socket access.
func ensureSELinuxPolicyConfigured(ctx context.Context) error {
	s := spinner.New("Configuring SELinux Podman socket policy")
	s.Start(ctx)

	if err := setupSELinuxPodmanSocketPolicy(); err != nil {
		s.Fail("failed to configure SELinux Podman socket policy")

		return err
	}
	s.Stop("SELinux Podman socket policy configured successfully")

	return nil
}
