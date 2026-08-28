package stream

import workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"

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
}
