package openshift

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
)

// Restore restores application data from a backup file for OpenShift runtime.
func (o *OpenshiftApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	return nil
}
