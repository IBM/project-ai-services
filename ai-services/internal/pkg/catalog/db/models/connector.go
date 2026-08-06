package models

import (
	"time"

	"github.com/google/uuid"
)

// ConnectorStatus represents the status of a connector.
type ConnectorStatus string

const (
	ConnectorStatusConnected ConnectorStatus = "Connected"
	ConnectorStatusOffline   ConnectorStatus = "Offline"
)

// Connector represents a tenant-level named remote content source registered in the catalog.
// Sensitive credential fields within Metadata are stored encrypted (see ST-9).
type Connector struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Provider  string          `json:"provider"`
	Status    ConnectorStatus `json:"status"`
	Message   string          `json:"message,omitempty"`
	Metadata  map[string]any  `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
