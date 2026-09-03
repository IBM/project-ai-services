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

type DownloadModel struct {
	Model string `json:"model"`
}

// ─── Network ──────────────────────────────────────────────────────────────────

type ListRoutes struct {
	LabelSelector string `json:"labelSelector"`
}

// ─── Caddy proxy management ───────────────────────────────────────────────────

// ProxyRouteOp identifies the specific Caddy operation within a single
// COMMAND_TYPE_PROXY_ROUTE command.
type ProxyRouteOp string

const (
	ProxyRouteOpRegister    ProxyRouteOp = "register"
	ProxyRouteOpUnregister  ProxyRouteOp = "unregister"
	ProxyRouteOpGet         ProxyRouteOp = "get"
	ProxyRouteOpHealthCheck ProxyRouteOp = "health_check"
)

// ProxyRoute is the unified payload for COMMAND_TYPE_PROXY_ROUTE.
// Op selects the operation; the remaining fields are populated as needed by
// each op (register uses all route fields; unregister/get use only ID;
// health_check uses none).
type ProxyRoute struct {
	Op       ProxyRouteOp `json:"op"`
	ID       string       `json:"id,omitempty"`
	Domain   string       `json:"domain,omitempty"`
	Upstream string       `json:"upstream,omitempty"`
	Terminal bool         `json:"terminal,omitempty"`
	Type     string       `json:"type,omitempty"`
}

// Route represents a Caddy reverse-proxy route on a worker node.
type Route struct {
	ID          string // unique route identifier used as @id in Caddy config
	Domain      string // hostname to match (e.g. "service.example.com")
	Upstream    string // backend address (e.g. "10.88.0.5:8080")
	Terminal    bool   // stop route matching after this route
	Type        string // endpoint type label (e.g. "ui", "api")
	ExternalURL string // fully-qualified HTTPS URL built by the worker from its own env
}

// ─── HTTP proxy ───────────────────────────────────────────────────────────────

// HTTPProxy is the request payload for COMMAND_TYPE_HTTP_PROXY.
// The control plane sends this; the worker executes the HTTP request locally
// against a pod endpoint and returns a types.HTTPProxyResponse.
type HTTPProxy struct {
	Method    string            `json:"method"`
	TargetURL string            `json:"target_url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      []byte            `json:"body,omitempty"`
}
