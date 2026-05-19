package models

import (
	"time"

	"github.com/google/uuid"
)

type Application struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Template  string    `json:"template"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Service struct {
	ID        uuid.UUID              `json:"id"`
	AppID     uuid.UUID              `json:"app_id"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status"`
	Endpoints map[string]interface{} `json:"endpoints"`
	Version   string                 `json:"version"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}
