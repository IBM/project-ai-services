package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
)

// Backup creates a backup of application data for OpenShift runtime.
// Currently not supported for OpenShift runtime.
func (o *OpenshiftApplication) Backup(ctx context.Context, opts types.BackupOptions) error {
	return fmt.Errorf("backup is not yet supported for OpenShift runtime")
}

// Made with Bob
