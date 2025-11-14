package bootstrap

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/bootstrap/root"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/bootstrap/spyre"
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
				return fmt.Errorf("Bootstrap configuration failed: %w", err)
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

	// 1. Install and configure Podman if not done
	// 1.1 Install Podman
	if _, err := validators.Podman(); err != nil {
		// setup podman socket and enable service
		logger.Infof("Podman not installed. Installing Podman...")
		if err := installPodman(); err != nil {
			return err
		}
	}

	// 1.2 Configure Podman
	if err := validators.PodmanHealthCheck(); err != nil {
		logger.Infof("Podman not configured. Configuring Podman...")
		if err := setupPodman(); err != nil {
			return err
		}
	} else {
		logger.Infof("Podman already configured")
	}
	// 2. Spyre cards – run servicereport tool to validate and repair spyre configurations
	if err := runServiceReport(); err != nil {
		return err
	} else {
		logger.Infof("Spyre cards configuration validated successfully.")
	}

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

	service_report_image := "icr.io/ai-services-private/tools:latest"
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
		service_report_image,
		"bash", "-c", "servicereport -r -p spyre",
	)
	out, err := svc_tool_cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run servicereport tool to validate Spyre cards configuration: %v, output: %s", err, string(out))
	}
	logger.Infof("ServiceReport output: %v", string(out))
	return nil
}

func installPodman() error {
	cmd := exec.Command("dnf", "-y", "install", "podman")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install podman: %v, output: %s", err, string(out))
	}
	logger.Infof("Podman installed successfully.")
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
