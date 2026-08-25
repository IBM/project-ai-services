// Package payload defines the JSON wire types used as Command.payload and
// CommandResult.data on the gRPC CommandStream between the control plane and
// worker nodes.
//
// Both sides of the stream — runtime/remote (control plane, sender) and
// worker/dispatch (worker node, receiver) — import this package so there is a
// single source of truth for field names and struct layout. Any change here
// automatically applies to both sides.
package payload

// ─── Image ────────────────────────────────────────────────────────────────────

type PullImage struct {
	Image string `json:"image"`
}

// ─── Pod ──────────────────────────────────────────────────────────────────────

type ListPods struct {
	Filters map[string][]string `json:"filters"`
}

type CreatePod struct {
	Body []byte            `json:"body"` // raw pod YAML
	Opts map[string]string `json:"opts"`
}

type DeletePod struct {
	ID    string `json:"id"`
	Force *bool  `json:"force,omitempty"`
}

// ─── Generic ──────────────────────────────────────────────────────────────────

// NameOrID is used by any method that takes a single name-or-ID argument.
type NameOrID struct {
	NameOrID string `json:"nameOrId"`
}

// Name is used by methods that take a plain name (DeleteSecret, DeleteVolume,
// DeletePVCs).
type Name struct {
	Name string `json:"name"`
}

// ─── Secret ───────────────────────────────────────────────────────────────────

type ListSecrets struct {
	Filters map[string][]string `json:"filters"`
}

// ─── Container ────────────────────────────────────────────────────────────────

type ExecInContainer struct {
	PodName       string   `json:"podName"`
	ContainerName string   `json:"containerName"`
	Command       []string `json:"command"`
}

// ─── Network ──────────────────────────────────────────────────────────────────

type ListRoutes struct {
	LabelSelector string `json:"labelSelector"`
}
