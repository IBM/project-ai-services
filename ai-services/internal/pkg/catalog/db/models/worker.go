package models

import (
	"time"

	"github.com/google/uuid"
)

// WorkerRuntimeType represents the execution environment of a worker.
type WorkerRuntimeType string

const (
	WorkerRuntimeTypePodman    WorkerRuntimeType = "podman"
	WorkerRuntimeTypeOpenShift WorkerRuntimeType = "openshift"
)

// WorkerStatus represents the lifecycle state of a worker.
type WorkerStatus string

const (
	WorkerStatusPending      WorkerStatus = "pending"
	WorkerStatusReady        WorkerStatus = "ready"
	WorkerStatusDisconnected WorkerStatus = "disconnected"
)

// Worker represents a registered worker agent.
type Worker struct {
	ID            uuid.UUID         `json:"id"`
	Name          string            `json:"name"`
	RuntimeType   WorkerRuntimeType `json:"runtime_type"`
	Status        WorkerStatus      `json:"status"`
	LastHeartbeat *time.Time        `json:"last_heartbeat,omitempty"`
	RegisteredAt  time.Time         `json:"registered_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}
