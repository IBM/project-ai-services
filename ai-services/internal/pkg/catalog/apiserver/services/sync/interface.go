package sync

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// PodStatus holds the relevant pod status information returned by a RuntimeSync.
type PodStatus struct {
	State   string
	Health  string
	PodName string
}

// ResourceValidationInput groups the runtime validation inputs for a single instance.
type ResourceValidationInput struct {
	AppID          string
	CatalogID      string
	InstanceID     string
	ItemType       string
	ActualPodCount int
}

// RuntimeSync is the interface that every runtime-specific sync backend must implement.
type RuntimeSync interface {
	// FetchPodStatuses returns the status of all pods associated with the given template ID.
	// The templateID is the service or component UUID used as the "ai-services.io/template" label value.
	// Returns an error when no pods are found or when the runtime call fails.
	FetchPodStatuses(ctx context.Context, rt runtime.Runtime, templateID string) ([]*PodStatus, error)

	// ValidateResources validates the expected runtime-managed resources for the given service or
	// component instance and returns an error message when a required resource is missing.
	ValidateResources(ctx context.Context, input ResourceValidationInput, rt runtime.Runtime) string
}
