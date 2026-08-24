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
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

const (
	// commandTimeout is the maximum time to wait for a worker to respond to a
	// single command. Long-running operations (e.g. PullImage) may need more
	// time; callers can pass a context with a tighter deadline if needed.
	commandTimeout = 5 * time.Minute
)

// RemoteRuntime implements runtime.Runtime by forwarding each call as a
// Command over the gRPC CommandStream to the named worker.
type RemoteRuntime struct {
	workerName  string
	runtimeType types.RuntimeType
	registry    WorkerRegistry
}

// New returns a RemoteRuntime targeting the named worker.
// runtimeType is the worker's declared runtime (stored in the DB at Register
// time) — used only by the Type() method; the gRPC protocol is runtime-agnostic.
func New(workerName string, runtimeType types.RuntimeType, reg WorkerRegistry) *RemoteRuntime {
	return &RemoteRuntime{
		workerName:  workerName,
		runtimeType: runtimeType,
		registry:    reg,
	}
}

// Type returns the runtime type declared by the worker at registration.
func (r *RemoteRuntime) Type() types.RuntimeType {
	return r.runtimeType
}

// ─── Image operations ─────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListImages() ([]types.Image, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_LIST_IMAGES, nil)
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
	_, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_PULL_IMAGE, pullImagePayload{Image: image})

	return err
}

// ─── Pod operations ───────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListPods(filters map[string][]string) ([]types.Pod, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_LIST_PODS, listPodsPayload{Filters: filters})
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

	res, err := r.send(ctx, workerpb.CommandType_COMMAND_TYPE_CREATE_POD, createPodPayload{Body: raw, Opts: opts})
	if err != nil {
		return nil, err
	}

	var pods []types.Pod
	if err := unmarshalData(res, &pods); err != nil {
		return nil, err
	}

	return pods, nil
}

func (r *RemoteRuntime) DeletePod(id string, force *bool) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_DELETE_POD, deletePodPayload{ID: id, Force: force})

	return err
}

func (r *RemoteRuntime) StopPod(id string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_STOP_POD, podIDPayload{ID: id})

	return err
}

func (r *RemoteRuntime) StartPod(id string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_START_POD, podIDPayload{ID: id})

	return err
}

func (r *RemoteRuntime) InspectPod(nameOrID string) (*types.Pod, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_INSPECT_POD, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var pod types.Pod
	if err := unmarshalData(res, &pod); err != nil {
		return nil, err
	}

	return &pod, nil
}

func (r *RemoteRuntime) PodExists(nameOrID string) (bool, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_POD_EXISTS, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

func (r *RemoteRuntime) PodLogs(nameOrID string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_POD_LOGS, nameOrIDPayload{NameOrID: nameOrID})

	return err
}

func (r *RemoteRuntime) GetPodResources(nameOrID string) (*types.PodResources, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_GET_POD_RESOURCES, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var pr types.PodResources
	if err := unmarshalData(res, &pr); err != nil {
		return nil, err
	}

	return &pr, nil
}

func (r *RemoteRuntime) GetNamespace() (string, error) {
	// Workers are always scoped to the default namespace — the namespace concept
	// only applies to OpenShift. Return empty string for podman workers.
	return "", nil
}

// ─── Secret operations ────────────────────────────────────────────────────────

func (r *RemoteRuntime) ListSecrets(filters map[string][]string) ([]string, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_LIST_SECRETS, listSecretsPayload{Filters: filters})
	if err != nil {
		return nil, err
	}

	var names []string
	if err := unmarshalData(res, &names); err != nil {
		return nil, err
	}

	return names, nil
}

func (r *RemoteRuntime) DeleteSecret(name string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_DELETE_SECRET, namePayload{Name: name})

	return err
}

func (r *RemoteRuntime) SecretExists(nameOrID string) (bool, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_SECRET_EXISTS, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

// UpdateSecret is not yet supported over the remote worker protocol.
// TODO: implement when required.
func (r *RemoteRuntime) UpdateSecret(_, _ string, _ map[string][]byte) error {
	return fmt.Errorf("remote runtime: UpdateSecret not yet supported on remote workers")
}

// ─── Volume operations ────────────────────────────────────────────────────────

func (r *RemoteRuntime) DeleteVolume(name string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_DELETE_VOLUME, namePayload{Name: name})

	return err
}

func (r *RemoteRuntime) VolumeExists(nameOrID string) (bool, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_VOLUME_EXISTS, nameOrIDPayload{NameOrID: nameOrID})
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

func (r *RemoteRuntime) InspectContainer(nameOrID string) (*types.Container, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_INSPECT_CONTAINER, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return nil, err
	}

	var c types.Container
	if err := unmarshalData(res, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *RemoteRuntime) ContainerExists(nameOrID string) (bool, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_CONTAINER_EXISTS, nameOrIDPayload{NameOrID: nameOrID})
	if err != nil {
		return false, err
	}

	var exists bool
	if err := unmarshalData(res, &exists); err != nil {
		return false, err
	}

	return exists, nil
}

func (r *RemoteRuntime) ContainerLogs(containerNameOrID string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_CONTAINER_LOGS, nameOrIDPayload{NameOrID: containerNameOrID})

	return err
}

func (r *RemoteRuntime) ExecInContainerWithCmd(podName, containerName string, command []string) (string, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_RUN_EPHEMERAL_CONTAINER,
		execInContainerPayload{PodName: podName, ContainerName: containerName, Command: command})
	if err != nil {
		return "", err
	}

	var output string
	if err := unmarshalData(res, &output); err != nil {
		return "", err
	}

	return output, nil
}

// ─── Network operations ───────────────────────────────────────────────────────

func (r *RemoteRuntime) ListRoutes(labelSelector string) ([]types.Route, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_LIST_ROUTES, labelSelectorPayload{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}

	var routes []types.Route
	if err := unmarshalData(res, &routes); err != nil {
		return nil, err
	}

	return routes, nil
}

// ─── CRD / namespace / PVC / system operations ────────────────────────────────

// ListCRD is not supported on remote workers (OpenShift-only).
func (r *RemoteRuntime) ListCRD(_ *unstructured.UnstructuredList, _ map[string][]string) ([]types.CRDResource, error) {
	return nil, fmt.Errorf("remote runtime: ListCRD not supported on remote workers")
}

// DeleteNamespace is not supported on remote workers (OpenShift-only).
func (r *RemoteRuntime) DeleteNamespace(_ string) error {
	return fmt.Errorf("remote runtime: DeleteNamespace not supported on remote workers")
}

func (r *RemoteRuntime) DeletePVCs(appLabel string) error {
	_, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_DELETE_PVCS, namePayload{Name: appLabel})

	return err
}

func (r *RemoteRuntime) GetSystemInfo() (*models.SystemInfo, error) {
	res, err := r.send(context.Background(), workerpb.CommandType_COMMAND_TYPE_GET_SYSTEM_INFO, nil)
	if err != nil {
		return nil, err
	}

	var info models.SystemInfo
	if err := unmarshalData(res, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// ─── send / receive ───────────────────────────────────────────────────────────

// send encodes payload as JSON, enqueues the Command on the worker's channel,
// and blocks until the worker returns a CommandResult or ctx/timeout expires.
func (r *RemoteRuntime) send(ctx context.Context, cmdType workerpb.CommandType, payload any) (*workerpb.CommandResult, error) {
	commandID := uuid.New().String()

	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("remote runtime: marshal payload for %s: %w", cmdType, err)
		}
	}

	// Register result channel BEFORE sending to avoid a race where the worker
	// responds before we start listening.
	resultCh, err := r.registry.WaitForResult(r.workerName, commandID)
	if err != nil {
		return nil, fmt.Errorf("remote runtime: worker %s not connected: %w", r.workerName, err)
	}

	cmdCh, ok := r.registry.WorkerCommandChannel(r.workerName)
	if !ok {
		return nil, fmt.Errorf("remote runtime: worker %s disconnected", r.workerName)
	}

	cmd := &workerpb.Command{
		CommandId: commandID,
		Type:      cmdType,
		Payload:   payloadBytes,
	}

	select {
	case cmdCh <- cmd:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	select {
	case res := <-resultCh:
		if !res.GetSuccess() {
			return nil, fmt.Errorf("remote runtime: worker %s: command %s failed: %s",
				r.workerName, cmdType, res.GetError())
		}

		return res, nil

	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("remote runtime: worker %s: command %s timed out after %s",
			r.workerName, cmdType, commandTimeout)
	}
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
