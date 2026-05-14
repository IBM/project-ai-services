package models

import (
	"time"

	"github.com/google/uuid"
)

// Service represents a service associated with an application.
type Service struct {
	ID        uuid.UUID         `json:"id"`
	AppID     uuid.UUID         `json:"app_id"`
	Type      string            `json:"type"`
	Status    ApplicationStatus `json:"status"`
	Endpoints map[string]any    `json:"endpoints,omitempty"`
	Version   string            `json:"version,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
