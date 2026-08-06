package sync

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// PodStatus holds the relevant pod status information returned by a RuntimeSync.
type PodStatus struct {
	State   string
	Health  string
	PodName string
}

// RuntimeSync is the interface that every runtime-specific sync backend must implement.
type RuntimeSync interface {
	// FetchPodStatuses returns the status of all pods associated with the given template ID.
	// The templateID is the service or component UUID used as the "ai-services.io/template" label value.
	// Returns an error when no pods are found or when the runtime call fails.
	FetchPodStatuses(rt runtime.Runtime, templateID string) ([]*PodStatus, error)

	// ShouldValidateResources reports whether this runtime supports template-based resource
	// count validation. Podman returns true; OpenShift returns false because Helm owns
	// all resource lifecycle and there are no .tmpl pod specs to count.
	ShouldValidateResources() bool
}
