package remote

// This file defines the JSON payload structs used as Command.payload
// and CommandResult.data for each CommandType.
//
// Naming convention: <verb><noun>Payload for request payloads.
// Response data is decoded directly into the method's return type (e.g. []types.Pod).
//
// NOTE: This file must stay in sync with internal/pkg/worker/dispatch/payloads.go —
// both sides of the gRPC stream must agree on JSON field names and struct layout.

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
