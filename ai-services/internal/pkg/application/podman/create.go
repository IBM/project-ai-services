package podman

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Create deploys a new application based on a template.
func (p *PodmanApplication) Create(ctx context.Context, opts types.CreateOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
