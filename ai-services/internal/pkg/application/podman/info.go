package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Info displays detailed information about an application.
func (p *PodmanApplication) Info(opts types.InfoOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
