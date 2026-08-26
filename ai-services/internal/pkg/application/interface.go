package application

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Application defines the interface for application lifecycle management operations.
type Application interface {
	// Create deploys a new application based on a template.
	Create(ctx context.Context, opts types.CreateOptions) error

	// Delete removes an application and its associated resources.
	Delete(ctx context.Context, opts types.DeleteOptions) error

	// Start starts a stopped application.
	Start(ctx context.Context, opts types.StartOptions) error

	// Stop stops a running application.
	Stop(ctx context.Context, opts types.StopOptions) error

	// List returns information about running applications.
	List(ctx context.Context, opts types.ListOptions) ([]types.ApplicationInfo, error)

	// Info displays detailed information about an application.
	Info(ctx context.Context, opts types.InfoOptions) error

	// Logs displays logs from an application pod.
	Logs(ctx context.Context, opts types.LogsOptions) error

	// Restore restores application data from a backup file.
	Restore(ctx context.Context, opts types.RestoreOptions) error

	// Backup creates a backup of application data.
	Backup(ctx context.Context, opts types.BackupOptions) error

	// Type returns the runtime type.
	Type() runtimeTypes.RuntimeType
}
