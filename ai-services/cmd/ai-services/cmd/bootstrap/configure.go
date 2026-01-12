package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/spyre"
	"github.com/spf13/cobra"
)

const (
	podmanSocketWaitDuration = 2 * time.Second
	contextTimeout           = 30 * time.Second
)

// configureCmd represents the validate subcommand of bootstrap.
func configureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "configure",
		Short:  "Configures the LPAR environment",
		Long:   `Configure and initialize the LPAR.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			logger.Infoln("Running bootstrap configuration...")

			err := RunConfigureCmd()
			if err != nil {
				return fmt.Errorf("bootstrap configuration failed: %w", err)
			}

			logger.Infof("Bootstrap configuration completed successfully.")

			return nil
		},
	}

	return cmd
}

func RunConfigureCmd() error {
	ctx := context.Background()

	s := spinner.New("Checking podman installation")
	s.Start(ctx)
	// 1. Install and configure Podman if not done
	// 1.1 Install Podman
	if _, err := validators.Podman(); err != nil {
		s.UpdateMessage("Installing podman")
		// setup podman socket and enable service
		if err := installPodman(); err != nil {
			s.Fail("failed to install podman")

			return err
		}
		s.Stop("podman installed successfully")
	} else {
		s.Stop("podman already installed")
	}

	s = spinner.New("Verifying podman configuration")
	s.Start(ctx)
	// 1.2 Configure Podman
	if err := validators.PodmanHealthCheck(); err != nil {
		s.UpdateMessage("Configuring podman")
		if err := setupPodman(); err != nil {
			s.Fail("failed to configure podman")

			return err
		}
		s.Stop("podman configured successfully")
	} else {
		s.Stop("Podman already configured")
	}

	s = spinner.New("Checking spyre card configuration")
	s.Start(ctx)
	// 2. Spyre cards – run servicereport tool to validate and repair spyre configurations
	if err := runServiceReport(); err != nil {
		s.Fail("failed to configure spyre card")

		return err
	}
	s.Stop("Spyre cards configuration validated successfully.")

	s = spinner.New("Setting up directories")
	s.Start(ctx)
	// 3. Setup directories
	if err := setupRequiredDirs(); err != nil {
		s.Fail("failed to setup directories")

		return err
	}
	s.Stop("Directories configured successfully")

	logger.Infoln("LPAR configured successfully")

	return nil
}

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
	euid := os.Geteuid()
	sudoUser := os.Getenv("SUDO_USER")

	if euid == 0 && sudoUser == "" {
		if err := systemctl("enable", "podman.socket", "--now"); err != nil {
			return fmt.Errorf("failed to enable podman socket: %w", err)
		}
	} else {
		machineArg := fmt.Sprintf("--machine=%s@.host", sudoUser)
		if err := systemctl("enable", "podman.socket", "--now", machineArg, "--user"); err != nil {
			return fmt.Errorf("failed to enable podman socket: %w", err)
		}
	}

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

func systemctl(action, unit string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	cmdArgs := []string{action}
	cmdArgs = append(cmdArgs, args...)
	cmdArgs = append(cmdArgs, unit)

	cmd := exec.CommandContext(ctx, "systemctl", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to %s %s: %v, output: %s", action, unit, err, string(out))
	}

	return nil
}

func setupRequiredDirs() error {
	dirs := []string{
		"/var/lib/ai-services",
		"/var/lib/ai-services/models",
		"/var/lib/ai-services/applications",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		logger.Infof("Created directory: %s", dir, logger.VerbosityLevelDebug)
	}

	sudoUser := os.Getenv("SUDO_USER")

	if sudoUser == "" {
		logger.Infoln("Running as root, directories will be owned by root", logger.VerbosityLevelDebug)
		return nil
	}

	u, err := user.Lookup(sudoUser)
	if err != nil {
		return fmt.Errorf("failed to lookup user %s: %w", err)
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("failed to parse UID: %w", err)
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("failed to parse UID: %w", err)
	}

	for _, dir := range dirs {
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("failed to set ownership for %s: %w", dir, err)
		}
		logger.Infof("Set ownership of %s to %s (UID: %d, GID: %d)", dir, sudoUser, uid, gid, logger.VerbosityLevelDebug)
	}

	for _, dir := range dirs {
		if err := chownRecursive(dir, uid, gid); err != nil {
			logger.Errorf("Failed to set ownership for %s: %v", dir, err)
		}
	}

	logger.Infof("Directory setup completed successfully for user: %s", sudoUser)

	return nil
}

func chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, uid, gid)
	})
}
