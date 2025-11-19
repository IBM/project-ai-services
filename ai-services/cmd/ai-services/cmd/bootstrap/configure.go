package bootstrap

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/root"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// validateCmd represents the validate subcommand of bootstrap
func configureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "configure",
		Short:  "configures the LPAR environment",
		Long:   `Configure and initialize the LPAR.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {

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
	rootCheck := root.NewRootRule()
	if err := rootCheck.Verify(); err != nil {
		return err
	}
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
	logger.Infoln("VFIO kernel modules loaded on the host", 2)

	svc_tool_cmd := exec.Command(
		"podman",
		"run",
		"--privileged",
		"--rm",
		"--name", "servicereport",
		"-v", "/etc/modprobe.d:/etc/modprobe.d",
		"-v", "/etc/modules-load.d/:/etc/modules-load.d/",
		"-v", "/etc/udev/rules.d/:/etc/udev/rules.d/",
		"-v", "/etc/security/limits.d/:/etc/security/limits.d/",
		vars.ToolImage,
		"bash", "-c", "servicereport -r -p spyre",
	)
	out, err := svc_tool_cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run servicereport tool to validate Spyre cards configuration: %v, output: %s", err, string(out))
	}
	logger.Infof("ServiceReport output: %v", string(out))

	if err := configureUsergroup(); err != nil {
		return err
	}

	return nil
}

func configureUsergroup() error {
	cmd_str := `groupadd sentient; usermod -aG sentient $USER`
	cmd := exec.Command("bash", "-c", cmd_str)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("❌ failed to create sentient group and add current user to the sentient group. Error: %w, output: %w", err, string(out))
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

	klog.V(2).Info("Waiting for podman socket to be ready...")
	time.Sleep(2 * time.Second) // wait for socket to be ready

	if err := validators.PodmanHealthCheck(); err != nil {
		return fmt.Errorf("podman health check failed after configuration: %w", err)
	}

	logger.Infof("Podman configured successfully.")
	return nil
}

func systemctl(action, unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", action, unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to %s %s: %v, output: %s", action, unit, err, string(out))
	}
	return nil
}
