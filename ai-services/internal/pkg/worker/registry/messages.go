package registry

// Worker status messages written to the database on lifecycle transitions.
const (
	// MsgDisconnected is set when a worker's gRPC stream closes cleanly.
	MsgDisconnected = "disconnected — connection stream closed"

	// MsgLostHeartbeat is set by the sweeper when a worker stops sending heartbeats.
	MsgLostHeartbeat = "lost heartbeat — no ping received within the timeout window"
)
