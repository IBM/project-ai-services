package podman

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/root"
)

const (
	podmanSocketWaitDuration = 2 * time.Second
	contextTimeout           = 30 * time.Second
)

// Configure performs the complete configuration of the Podman environment.
func (p *PodmanBootstrap) Configure() error {
	rootCheck := root.NewRootRule()
	if err := rootCheck.Verify(); err != nil {
		return err
	}
	ctx := context.Background()

	s := spinner.New("Checking spyre card configuration")
	s.Start(ctx)
	// 1. Spyre cards – validate and repair spyre configurations
	if err := configureSpyre(); err != nil {
		s.Fail("failed to configure spyre card")

		return err
	}
	s.Stop("Spyre cards configuration validated successfully.")

	s = spinner.New("Checking podman installation")
	s.Start(ctx)
	// 2. Install and configure Podman if not done
	// 2.1 Install Podman
	if _, err := utils.Podman(); err != nil {
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
	// 2.2 Configure Podman
	if err := utils.PodmanHealthCheck(); err != nil {
		s.UpdateMessage("Configuring podman")
		if err := setupPodman(); err != nil {
			s.Fail("failed to configure podman")

			return err
		}
		s.Stop("podman configured successfully")
	} else {
		s.Stop("Podman already configured")
	}

	if err := configurePodmanGroups(); err != nil {
		return fmt.Errorf("failed to configure podman service supplementary groups: %w", err)
	}
	s = spinner.New("Configuring SMT level to 2")
	s.Start(ctx)
	// 3. Configure SMT level to 2 and persist via systemd
	if err := setupSMTLevel(); err != nil {
		s.Fail("failed to configure SMT level")

		return err
	}
	s.Stop("SMT level configured successfully (set to 2)")

	logger.Infoln("LPAR configured successfully")

	return nil
}

// Made with Bob
