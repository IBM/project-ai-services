package models

import (
	"time"

	"github.com/google/uuid"
)

// Component represents a reusable component in the catalog.
// Components are infrastructure pieces that can be shared across multiple services,
// such as LLM servers, embedding models, vector databases, etc.
type Component struct {
	ID        uuid.UUID      `json:"id"`
	Type      string         `json:"type"`                // e.g., "llm", "embedding", "vector_db", "reranker"
	Provider  string         `json:"provider"`            // e.g., "vllm-cpu", "vllm-spyre"
	Endpoints map[string]any `json:"endpoints,omitempty"` // JSONB field for endpoint configurations
	Version   string         `json:"version,omitempty"`   // Component version
	Metadata  map[string]any `json:"metadata,omitempty"`  // JSONB field for additional metadata
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Made with Bob
