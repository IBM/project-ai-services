package models

import (
	"time"

	"github.com/google/uuid"
)

// Service represents a service associated with an application.
type Service struct {
	ID        uuid.UUID        `json:"id"`
	AppID     uuid.UUID        `json:"app_id"`
	CatalogID string           `json:"catalog_id"`
	Status    ServiceStatus    `json:"status"`
	Endpoints []map[string]any `json:"endpoints,omitempty"`
	Component Component        `json:"component,omitempty"`
	Version   string           `json:"version,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}
