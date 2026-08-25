package types

import "time"

// WorkerStatus represents the lifecycle state of a worker as seen by API callers.
type WorkerStatus string

// WorkerRuntimeType represents the execution environment declared by a worker.
type WorkerRuntimeType string

// Worker is the public API representation of a registered worker.
// It uses plain strings for timestamps so the wire format is stable
// regardless of DB model changes.
type Worker struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	RuntimeType   WorkerRuntimeType `json:"runtime_type"`
	Status        WorkerStatus      `json:"status"`
	LastHeartbeat *time.Time        `json:"last_heartbeat,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
	RegisteredAt  string            `json:"registered_at"`
	UpdatedAt     string            `json:"updated_at"`
}
