// Package remote provides RemoteRuntime, a runtime.Runtime implementation that
// forwards every method call to a connected worker daemon over the gRPC
// CommandStream. The control plane uses this to drive deployments on remote
// worker nodes without caring whether the worker runs podman or openshift —
// that knowledge lives only in the worker's local dispatcher.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// RemoteRuntime implements runtime.Runtime by forwarding each call as a
// Command over the gRPC CommandStream to the named worker.
type RemoteRuntime struct {
	*stream.Sender
	runtimeType types.RuntimeType
	// namespace is the target namespace for namespace-scoped operations on
	// OpenShift (e.g. ListPods, ListRoutes, DeletePVCs). Empty for Podman.
	namespace string
}

// New returns a RemoteRuntime targeting the named worker.
// runtimeType is the worker's declared runtime (stored in the DB at Register
// time) — used only by the Type() method; the gRPC protocol is runtime-agnostic.
func New(workerName string, runtimeType types.RuntimeType, reg stream.WorkerRegistry) *RemoteRuntime {
	return &RemoteRuntime{
		Sender:      stream.New(workerName, reg),
		runtimeType: runtimeType,
	}
}

// WithNamespace returns a shallow copy of the RemoteRuntime scoped to the
// given namespace. Use this when driving operations against an application
// namespace on an OpenShift worker.
func (r *RemoteRuntime) WithNamespace(ns string) *RemoteRuntime {
	copy := *r
	copy.namespace = ns

	return &copy
}

// Type returns the runtime type declared by the worker at registration.
func (r *RemoteRuntime) Type() types.RuntimeType {
	return r.runtimeType
}

// ─── Image operations ─────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListImages(ctx context.Context) ([]types.Image, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_LIST_IMAGES, nil)
	if err != nil {
		return nil, err
	}

	var images []types.Image
	if err := unmarshalData(res, &images); err != nil {
		return nil, err
	}

	return images, nil
}

func (r *RemoteRuntime) PullImage(ctx context.Context, image string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_PULL_IMAGE, payload.PullImage{Image: image})

	return err
}

// ─── Pod operations ───────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListPods(ctx context.Context, filters map[string][]string) ([]types.Pod, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_LIST_PODS, payload.ListPods{Namespace: r.namespace, Filters: filters})
	if err != nil {
		return nil, err
	}

	var pods []types.Pod
	if err := unmarshalData(res, &pods); err != nil {
		return nil, err
	}

	return pods, nil
}

func (r *RemoteRuntime) CreatePod(ctx context.Context, body io.Reader, opts map[string]string) ([]types.Pod, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("remote runtime: read pod body: %w", err)
	}

	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_CREATE_POD, payload.CreatePod{Body: raw, Opts: opts})
	if err != nil {
		return nil, err
	}

	var pods []types.Pod
	if err := unmarshalData(res, &pods); err != nil {
		return nil, err
	}

	return pods, nil
}

func (r *RemoteRuntime) DeletePod(ctx context.Context, nameOrID string, force *bool) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_DELETE_POD, payload.DeletePod{ID: nameOrID, Force: force})

	return err
}

func (r *RemoteRuntime) StopPod(ctx context.Context, nameOrID string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_STOP_POD, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})

	return err
}

func (r *RemoteRuntime) StartPod(ctx context.Context, nameOrID string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_START_POD, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})

	return err
}

func (r *RemoteRuntime) InspectPod(ctx context.Context, nameOrID string) (*types.Pod, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_INSPECT_POD, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var pod types.Pod
	if err := unmarshalData(res, &pod); err != nil {
		return nil, err
	}

	return &pod, nil
}

func (r *RemoteRuntime) PodExists(ctx context.Context, nameOrID string) (bool, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_POD_EXISTS, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

func (r *RemoteRuntime) PodLogs(ctx context.Context, nameOrID string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_POD_LOGS, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})

	return err
}

func (r *RemoteRuntime) GetPodResources(ctx context.Context, nameOrID string) (*types.PodResources, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_GET_POD_RESOURCES, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var pr types.PodResources
	if err := unmarshalData(res, &pr); err != nil {
		return nil, err
	}

	return &pr, nil
}

func (r *RemoteRuntime) GetNamespace(_ context.Context) (string, error) {
	// Workers are always scoped to the default namespace — the namespace concept
	// only applies to OpenShift. Return empty string for podman workers.
	return "", nil
}

// ─── Secret operations ────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListSecrets(ctx context.Context, filters map[string][]string) ([]string, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_LIST_SECRETS, payload.ListSecrets{Namespace: r.namespace, Filters: filters})
	if err != nil {
		return nil, err
	}

	var names []string
	if err := unmarshalData(res, &names); err != nil {
		return nil, err
	}

	return names, nil
}

func (r *RemoteRuntime) DeleteSecret(ctx context.Context, name string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_DELETE_SECRET, payload.Name{Namespace: r.namespace, Name: name})

	return err
}

func (r *RemoteRuntime) SecretExists(ctx context.Context, nameOrID string) (bool, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_SECRET_EXISTS, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

// UpdateSecret forwards a secret update + deployment restart to the remote OpenShift worker.
func (r *RemoteRuntime) UpdateSecret(ctx context.Context, name, deploymentName string, data map[string][]byte) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_UPDATE_SECRET, payload.UpdateSecret{
		Namespace:      r.namespace,
		Name:           name,
		DeploymentName: deploymentName,
		Data:           data,
	})

	return err
}

// ─── Volume operations ────────────────────────────────────────────────────────

func (r *RemoteRuntime) DeleteVolume(ctx context.Context, name string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_DELETE_VOLUME, payload.Name{Namespace: r.namespace, Name: name})

	return err
}

func (r *RemoteRuntime) VolumeExists(ctx context.Context, nameOrID string) (bool, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_VOLUME_EXISTS, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

// ─── Container operations ─────────────────────────────────────────────────────

func (r *RemoteRuntime) InspectContainer(ctx context.Context, nameOrID string) (*types.Container, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_INSPECT_CONTAINER, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var c types.Container
	if err := unmarshalData(res, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *RemoteRuntime) ContainerExists(ctx context.Context, nameOrID string) (bool, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_CONTAINER_EXISTS, payload.NameOrID{Namespace: r.namespace, NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

func (r *RemoteRuntime) ContainerLogs(ctx context.Context, containerNameOrID string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_CONTAINER_LOGS, payload.NameOrID{Namespace: r.namespace, NameOrID: containerNameOrID})

	return err
}

func (r *RemoteRuntime) ExecInContainerWithCmd(ctx context.Context, podName, containerName string, command []string) (string, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_EXEC_IN_CONTAINER,
		payload.ExecInContainer{Namespace: r.namespace, PodName: podName, ContainerName: containerName, Command: command})
	if err != nil {
		return "", err
	}

	var output string
	if err := unmarshalData(res, &output); err != nil {
		return "", err
	}

	return output, nil
}

// ─── HTTP proxy tunnel ────────────────────────────────────────────────────────

// HTTPProxy tunnels an HTTP request to the worker via the gRPC stream.
// The worker executes the request locally (inside the Podman network) and
// returns the status, headers, and body as a single CommandResult.
func (r *RemoteRuntime) HTTPProxy(ctx context.Context, method, targetURL string, headers map[string]string, body []byte) (*types.HTTPProxyResponse, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_HTTP_PROXY,
		payload.HTTPProxy{
			Method:    method,
			TargetURL: targetURL,
			Headers:   headers,
			Body:      body,
		})
	if err != nil {
		return nil, err
	}

	var result types.HTTPProxyResponse
	if err := unmarshalData(res, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ─── Network operations ───────────────────────────────────────────────────────

func (r *RemoteRuntime) ListRoutes(ctx context.Context, labelSelector string) ([]types.Route, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_LIST_ROUTES, payload.ListRoutes{Namespace: r.namespace, LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}

	var routes []types.Route
	if err := unmarshalData(res, &routes); err != nil {
		return nil, err
	}

	return routes, nil
}

// ─── OpenShift-specific operations ───────────────────────────────────────────

// WaitForInferenceServiceReady forwards a KServe InferenceService readiness wait
// to the remote OpenShift worker. The worker polls the InferenceService until
// Ready=True or the context deadline is exceeded. The context timeout is
// authoritative — the worker respects ctx.Done() via the gRPC deadline.
func (r *RemoteRuntime) WaitForInferenceServiceReady(ctx context.Context, isvcName string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_WAIT_INFERENCE_SERVICE, payload.WaitInferenceService{
		Namespace: r.namespace,
		Name:      isvcName,
	})

	return err
}

// ─── CRD / namespace / PVC / system operations ────────────────────────────────

// ListCRD forwards a CRD list query to the remote OpenShift worker.
// The GVK is extracted from list and sent alongside the namespace and label filters.
func (r *RemoteRuntime) ListCRD(ctx context.Context, list *unstructured.UnstructuredList, filters map[string][]string) ([]types.CRDResource, error) {
	gvk := list.GroupVersionKind()
	labelKeys := filters["label"]

	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_LIST_CRD, payload.ListCRD{
		Namespace: r.namespace,
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		LabelKeys: labelKeys,
	})
	if err != nil {
		return nil, err
	}

	var items []payload.CRDResource
	if err := unmarshalData(res, &items); err != nil {
		return nil, err
	}

	result := make([]types.CRDResource, len(items))
	for i, item := range items {
		result[i] = types.CRDResource{Name: item.Name, Labels: item.Labels}
	}

	return result, nil
}

// DeleteNamespace forwards a namespace deletion to the remote OpenShift worker.
func (r *RemoteRuntime) DeleteNamespace(ctx context.Context, name string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_DELETE_NAMESPACE, payload.DeleteNamespace{Name: name})

	return err
}

func (r *RemoteRuntime) DeletePVCs(ctx context.Context, appLabel string) error {
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_DELETE_PVCS, payload.Name{Namespace: r.namespace, Name: appLabel})

	return err
}

func (r *RemoteRuntime) GetSystemInfo(ctx context.Context) (*models.SystemInfo, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_GET_SYSTEM_INFO, nil)
	if err != nil {
		return nil, err
	}

	var info models.SystemInfo
	if err := unmarshalData(res, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// FindFreeSpyreCards returns free Spyre PCI addresses discovered on the worker host.
func (r *RemoteRuntime) FindFreeSpyreCards(ctx context.Context) ([]string, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_FIND_FREE_SPYRE_CARDS, nil)
	if err != nil {
		return nil, err
	}

	var cards []string
	if err := unmarshalData(res, &cards); err != nil {
		return nil, err
	}

	return cards, nil
}

// GetBaseDir returns the AI services base directory configured on the worker host.
func (r *RemoteRuntime) GetBaseDir(ctx context.Context) (string, error) {
	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_GET_BASE_DIR, nil)
	if err != nil {
		return "", err
	}

	var baseDir string
	if err := unmarshalData(res, &baseDir); err != nil {
		return "", err
	}

	return baseDir, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// send delegates to the embedded Sender for convenience.
func (r *RemoteRuntime) send(ctx context.Context, cmdType workerpb.CommandType, p any) (*workerpb.CommandResult, error) {
	return r.Send(ctx, cmdType, p)
}

// unmarshalData decodes CommandResult.data into v.
func unmarshalData(res *workerpb.CommandResult, v any) error {
	if len(res.GetData()) == 0 {
		return nil
	}

	if err := json.Unmarshal(res.GetData(), v); err != nil {
		return fmt.Errorf("remote runtime: unmarshal response: %w", err)
	}

	return nil
}
