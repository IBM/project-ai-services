package dispatch

// This file mirrors internal/pkg/runtime/remote/payloads.go exactly.
// Both sides of the gRPC stream must agree on the JSON field names and
// struct layout. Any change here must be reflected in the remote package.

// ─── Image ────────────────────────────────────────────────────────────────────

type pullImagePayload struct {
	Image string `json:"image"`
}

// ─── Pod ──────────────────────────────────────────────────────────────────────

type listPodsPayload struct {
	Filters map[string][]string `json:"filters"`
}

type createPodPayload struct {
	Body []byte            `json:"body"` // raw pod YAML
	Opts map[string]string `json:"opts"`
}

type deletePodPayload struct {
	ID    string `json:"id"`
	Force *bool  `json:"force,omitempty"`
}

// podIDPayload is used by StopPod and StartPod.
type podIDPayload struct {
	ID string `json:"id"`
}

// ─── Generic ──────────────────────────────────────────────────────────────────

// nameOrIDPayload is used by any method that takes a single name-or-ID string.
type nameOrIDPayload struct {
	NameOrID string `json:"nameOrId"`
}

// namePayload is used by methods that take a plain name (not an ID).
type namePayload struct {
	Name string `json:"name"`
}

// ─── Secret ───────────────────────────────────────────────────────────────────

type listSecretsPayload struct {
	Filters map[string][]string `json:"filters"`
}

// ─── Container ────────────────────────────────────────────────────────────────

type execInContainerPayload struct {
	PodName       string   `json:"podName"`
	ContainerName string   `json:"containerName"`
	Command       []string `json:"command"`
}

// ─── Network ──────────────────────────────────────────────────────────────────

type labelSelectorPayload struct {
	LabelSelector string `json:"labelSelector"`
}
