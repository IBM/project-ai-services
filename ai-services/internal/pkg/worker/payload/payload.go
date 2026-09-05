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
	Namespace string              `json:"namespace,omitempty"`
	Filters   map[string][]string `json:"filters"`
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
	Namespace string `json:"namespace,omitempty"`
	NameOrID  string `json:"nameOrId"`
}

// Name is used by methods that take a plain name (DeleteSecret, DeleteVolume,
// DeletePVCs).
type Name struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// ─── Secret ───────────────────────────────────────────────────────────────────

type ListSecrets struct {
	Namespace string              `json:"namespace,omitempty"`
	Filters   map[string][]string `json:"filters"`
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
	Namespace     string `json:"namespace,omitempty"`
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

// ─── Helm ─────────────────────────────────────────────────────────────────────

// ChartFile is a single file within a serialised Helm chart, used to transmit
// chart content across process boundaries (e.g. over the gRPC CommandStream).
// Name is the path relative to the chart root (e.g. "Chart.yaml",
// "templates/deployment.yaml"). Data is the raw file content.
type ChartFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// HelmInstall is the wire payload for COMMAND_TYPE_HELM_INSTALL.
type HelmInstall struct {
	Release    string         `json:"release"`
	Namespace  string         `json:"namespace"`
	ChartFiles []ChartFile    `json:"chart_files"`
	Values     map[string]any `json:"values"`
	TemplateID string         `json:"template_id,omitempty"`
	TimeoutSec int64          `json:"timeout_sec,omitempty"`
}

// HelmRelease identifies a Helm release by name and namespace.
// Used as the wire payload for commands that operate on an existing release
// (COMMAND_TYPE_HELM_UNINSTALL, COMMAND_TYPE_HELM_GET_MANIFEST).
type HelmRelease struct {
	Release   string `json:"release"`
	Namespace string `json:"namespace"`
}

// HelmManifest is the wire response for COMMAND_TYPE_HELM_GET_MANIFEST.
type HelmManifest struct {
	Manifest string `json:"manifest"`
}

// ListCRD is the wire payload for COMMAND_TYPE_LIST_CRD.
// Group, Version, and Kind identify the CRD type to list.
// Namespace scopes the query. LabelKeys filters resources by label key presence.
type ListCRD struct {
	Namespace string   `json:"namespace"`
	Group     string   `json:"group"`
	Version   string   `json:"version"`
	Kind      string   `json:"kind"`
	LabelKeys []string `json:"label_keys,omitempty"`
}

// CRDResource is the wire representation of a single CRD resource returned
// by COMMAND_TYPE_LIST_CRD.
type CRDResource struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

// DeleteNamespace is the wire payload for COMMAND_TYPE_DELETE_NAMESPACE.
type DeleteNamespace struct {
	Name string `json:"name"`
}

// UpdateSecret is the wire payload for COMMAND_TYPE_UPDATE_SECRET.
type UpdateSecret struct {
	Namespace      string            `json:"namespace"`
	Name           string            `json:"name"`
	DeploymentName string            `json:"deployment_name"`
	Data           map[string][]byte `json:"data"`
}

// WaitInferenceService is the wire payload for COMMAND_TYPE_WAIT_INFERENCE_SERVICE.
// The worker polls the named KServe InferenceService in Namespace until its
// Ready condition is True, or until the caller's context deadline is exceeded.
type WaitInferenceService struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
