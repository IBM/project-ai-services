package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Start starts a stopped application.
func (p *PodmanApplication) Start(opts types.StartOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
