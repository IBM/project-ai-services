package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// List returns information about running applications.
func (p *PodmanApplication) List(opts types.ListOptions) ([]types.ApplicationInfo, error) {
	logger.Warningln("yet to implement")

	return nil, nil
}
