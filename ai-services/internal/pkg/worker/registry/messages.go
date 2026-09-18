package registry

// Worker status messages written to the database on lifecycle transitions.
const (
	// MsgPendingRegistration is set at pre-registration time, before the worker
	// connects and completes setup.
	MsgPendingRegistration = "Registration is not complete. Run the 'worker join' command on the worker to connect it and finish setup."

	// MsgDisconnected is set when a worker's gRPC stream closes cleanly.
	MsgDisconnected = "disconnected — connection stream closed."

	// MsgLostHeartbeat is set by the sweeper when a worker stops sending heartbeats.
	MsgLostHeartbeat = "lost heartbeat — no ping received within the timeout window."
)
