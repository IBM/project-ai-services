package stream

import (
	"context"

	"github.com/google/uuid"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// WorkerRegistry is the interface both Sender and RemoteRuntime require.
// *worker/registry.Registry satisfies it automatically.
type WorkerRegistry interface {
	// WaitForResult registers a result channel for commandID on workerName and
	// returns it. The channel receives exactly one value when the worker replies.
	WaitForResult(workerName, commandID string) (chan *workerpb.CommandResult, error)

	// WorkerCommandChannel returns the command channel for the named worker, or
	// (nil, false) if the worker is not currently connected.
	WorkerCommandChannel(workerName string) (chan *workerpb.Command, bool)

	// WorkerRuntimeType returns the runtime type string declared by the worker at
	// registration time, or ("", false) if the worker is not connected.
	WorkerRuntimeType(workerName string) (string, bool)

	// WorkerMetadata returns the registration metadata for the named worker, or
	// (nil, false) if the worker is not connected.
	WorkerMetadata(workerName string) (map[string]string, bool)

	// WorkerID returns the database UUID of the named worker, or (uuid.Nil, false)
	// if the worker is not connected.
	WorkerID(workerName string) (uuid.UUID, bool)

	// WorkerNameByID returns the name of the worker with the given database UUID,
	// or ("", false) if no connected worker has that ID.
	WorkerNameByID(id uuid.UUID) (string, bool)

	// IsWorkerConnected reports whether the named worker has status=ready in the DB.
	IsWorkerConnected(ctx context.Context, workerName string) bool
}
