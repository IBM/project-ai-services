package podman

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	podmanSocketWaitDuration = 2 * time.Second
	contextTimeout           = 30 * time.Second
)

// Configure performs the complete configuration of the Podman environment.
func (p *PodmanBootstrap) Configure() error {
	euid := os.Geteuid()
	if euid != 0 {
		return fmt.Errorf("podman bootstrap requires root privileges, either run as root or use sudo")
	}

	ctx := context.Background()

	// 1. Install and configure Podman if not done
	if err := ensurePodmanInstalled(ctx); err != nil {
		return err
	}

	if err := ensurePodmanConfigured(ctx); err != nil {
		return err
	}

	// 2. Enable podman-restart service for auto-start on reboot
	if err := ensurePodmanRestartServiceEnabled(ctx); err != nil {
		return err
	}

	// 3. Spyre cards – validate and repair spyre configurations
	if err := ensureSpyreConfigured(ctx); err != nil {
		return err
	}

	// 4. Configure SMT level to 2 and persist via systemd
	if err := ensureSMTConfigured(ctx); err != nil {
		return err
	}

	// 5. Configure SELinux policy for Podman socket access
	if err := ensureSELinuxPolicyConfigured(ctx); err != nil {
		return err
	}

	logger.Infoln("LPAR configured successfully")

	return nil
}

// Made with Bob
