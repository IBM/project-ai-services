package podman

import (
	"context"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
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

// Made with Bob
