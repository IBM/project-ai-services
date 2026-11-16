package podman

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// PodmanDeleter removes the pods of a local model from a Podman runtime, local or remote over the
// worker stream.
type PodmanDeleter struct {
	componentRepo dbrepo.ComponentRepository
}

// NewPodmanDeleter creates a new PodmanDeleter.
func NewPodmanDeleter(componentRepo dbrepo.ComponentRepository) *PodmanDeleter {
	return &PodmanDeleter{componentRepo: componentRepo}
}

// Delete runs the same cleanup as the orphaned components of an application: pods, secrets and
// volumes, then the component row, which is kept if anything before it failed.
func (d *PodmanDeleter) Delete(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) error {
	if errs := podmanutils.DeleteComponents(ctx, rt, d.componentRepo, []uuid.UUID{componentID}, false); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}
