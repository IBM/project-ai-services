package podman

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/spyre"
)

func runServiceReport() error {
	// validate spyre attachment first before running servicereport
	spyreCheck := spyre.NewSpyreRule()
	err := spyreCheck.Verify()
	if err != nil {
		return err
	}

	// Create host directories for vfio
	cmd := `mkdir -p /etc/modules-load.d; mkdir -p /etc/udev/rules.d/`
	_, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to create host volume mounts for servicereport tool %w", err)
	}

	// load vfio kernel modules
	cmd = `modprobe vfio_pci`
	_, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to load vfio kernel modules for spyre %w", err)
	}
	logger.Infoln("VFIO kernel modules loaded on the host", logger.VerbosityLevelDebug)

	if err := helpers.RunServiceReportContainer("servicereport -r -p spyre", "configure"); err != nil {
		return err
	}

	if err := configureUsergroup(); err != nil {
		return err
	}

	if err := reloadUdevRules(); err != nil {
		return err
	}

	cards, err := helpers.ListSpyreCards()
	if err != nil || len(cards) == 0 {
		return fmt.Errorf("❌ failed to list spyre cards on LPAR %w", err)
	}
	num_spyre_cards := len(cards)

	// check if kernel modules for vfio are loaded
	if err := checkKernelModulesLoaded(num_spyre_cards); err != nil {
		return err
	}

	return nil
}

func configureUsergroup() error {
	cmd_str := `groupadd sentient; usermod -aG sentient $USER`
	cmd := exec.Command("bash", "-c", cmd_str)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create sentient group and add current user to the sentient group. Error: %w, output: %s", err, string(out))
	}

	return nil
}

func reloadUdevRules() error {
	cmd := `udevadm control --reload-rules`
	_, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("failed to reload udev rules. Error: %w", err)
	}

	return nil
}

func checkKernelModulesLoaded(num_spyre_cards int) error {
	vfio_cmd := `lspci -k -d 1014:06a7 | grep "Kernel driver in use: vfio-pci" | wc -l`
	out, err := exec.Command("bash", "-c", vfio_cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to check vfio cards with kernel modules loaded %w", err)
	}

	num_vf_cards, err := strconv.Atoi(strings.TrimSuffix(string(out), "\n"))
	if err != nil {
		return fmt.Errorf("❌ failed to convert number of virtual spyre cards count from string to integer %w", err)
	}

	if num_vf_cards != num_spyre_cards {
		logger.Infof("failed to detect vfio cards, reloading vfio kernel modules..")
		// reload vfio kernel modules
		cmd := `rmmod vfio_pci; modprobe vfio_pci`
		_, err = exec.Command("bash", "-c", cmd).Output()
		if err != nil {
			return fmt.Errorf("❌ failed to reload vfio kernel modules for spyre %w", err)
		}
		logger.Infoln("VFIO kernel modules reloaded on the host", logger.VerbosityLevelDebug)
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
