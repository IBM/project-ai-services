// Package dispatch implements the worker-side command dispatcher.
//
// When the gRPC CommandStream delivers a Command from the control plane, the
// worker calls Dispatch: the payload is decoded, the appropriate local
// runtime.Runtime method is called, and a CommandResult is returned to be
// sent back on the stream.
//
// The dispatcher is intentionally runtime-agnostic — the same Command envelope
// is used for podman and openshift; the runtime implementation handles the
// underlying differences transparently.
package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	helmutil "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	openshiftRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// Dispatch routes cmd to the appropriate local runtime method and returns the
// CommandResult to send back on the stream. It never returns an error — all
// failures are encoded as CommandResult{Success: false, Error: "..."} so the
// control plane always gets a response and its blocking send() can unblock.
// pr may be nil for runtimes that do not support proxy route management (e.g. OpenShift).
func Dispatch(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, cmd *workerpb.Command) *workerpb.CommandResult {
	data, err := handle(ctx, rt, pr, cmd)
	if err != nil {
		return failResult(cmd.GetCommandId(), err)
	}

	return okResult(cmd.GetCommandId(), data)
}

// defaultHelmTimeout is used when the caller does not supply a timeout.
const defaultHelmTimeout = 10 * time.Minute

// ─── router ───────────────────────────────────────────────────────────────────

//nolint:gocognit,cyclop,funlen // large switch is unavoidable for a flat dispatch table
func handle(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, cmd *workerpb.Command) ([]byte, error) {
	p := cmd.GetPayload()

	switch cmd.GetType() {
	// ── Images ────────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_LIST_IMAGES:
		images, err := rt.ListImages(ctx)

		return marshalOr(images, err)

	case workerpb.CommandType_COMMAND_TYPE_PULL_IMAGE:
		var req payload.PullImage
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode pull_image payload: %w", err)
		}

		return nil, rt.PullImage(ctx, req.Image)

	// ── Pods ──────────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_LIST_PODS:
		var req payload.ListPods
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode list_pods payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		pods, err := nrt.ListPods(ctx, req.Filters)

		return marshalOr(pods, err)

	case workerpb.CommandType_COMMAND_TYPE_CREATE_POD:
		var req payload.CreatePod
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode create_pod payload: %w", err)
		}
		pods, err := rt.CreatePod(ctx, bytes.NewReader(req.Body), req.Opts)

		return marshalOr(pods, err)

	case workerpb.CommandType_COMMAND_TYPE_DELETE_POD:
		var req payload.DeletePod
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_pod payload: %w", err)
		}

		return nil, rt.DeletePod(ctx, req.ID, req.Force)

	case workerpb.CommandType_COMMAND_TYPE_STOP_POD:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode stop_pod payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.StopPod(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_START_POD:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode start_pod payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.StartPod(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_INSPECT_POD:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode inspect_pod payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		pod, err := nrt.InspectPod(ctx, req.NameOrID)

		return marshalOr(pod, err)

	case workerpb.CommandType_COMMAND_TYPE_POD_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode pod_exists payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		exists, err := nrt.PodExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	case workerpb.CommandType_COMMAND_TYPE_POD_LOGS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode pod_logs payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.PodLogs(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_GET_POD_RESOURCES:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode get_pod_resources payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		pr, err := nrt.GetPodResources(ctx, req.NameOrID)

		return marshalOr(pr, err)

	// ── Secrets ───────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_LIST_SECRETS:
		var req payload.ListSecrets
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode list_secrets payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		names, err := nrt.ListSecrets(ctx, req.Filters)

		return marshalOr(names, err)

	case workerpb.CommandType_COMMAND_TYPE_DELETE_SECRET:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_secret payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.DeleteSecret(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_SECRET_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode secret_exists payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		exists, err := nrt.SecretExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	// ── Volumes ───────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_DELETE_VOLUME:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_volume payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.DeleteVolume(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_VOLUME_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode volume_exists payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		exists, err := nrt.VolumeExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	// ── Containers ────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_INSPECT_CONTAINER:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode inspect_container payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		c, err := nrt.InspectContainer(ctx, req.NameOrID)

		return marshalOr(c, err)

	case workerpb.CommandType_COMMAND_TYPE_CONTAINER_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode container_exists payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		exists, err := nrt.ContainerExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	case workerpb.CommandType_COMMAND_TYPE_CONTAINER_LOGS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode container_logs payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.ContainerLogs(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_EXEC_IN_CONTAINER:
		var req payload.ExecInContainer
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode exec_in_container payload: %w", err)
		}
		out, err := rt.ExecInContainerWithCmd(ctx, req.PodName, req.ContainerName, req.Command)

		return marshalOr(out, err)

	case workerpb.CommandType_COMMAND_TYPE_DOWNLOAD_MODEL:
		var req payload.DownloadModel
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode download_model payload: %w", err)
		}

		return nil, helpers.DownloadModelContainer(ctx, req.Model, utils.GetModelsPath())

	// ── Caddy proxy management ────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_PROXY_ROUTE:
		if pr == nil {
			return nil, fmt.Errorf("proxy route management not supported")
		}

		var req payload.ProxyRoute
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode proxy_route payload: %w", err)
		}

		route, err := pr.ManageProxyRoute(ctx, req.Op, payload.Route{
			ID:       req.ID,
			Domain:   req.Domain,
			Upstream: req.Upstream,
			Terminal: req.Terminal,
			Type:     req.Type,
		})

		return marshalOr(route, err)

	// ── HTTP proxy tunnel ──────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_HTTP_PROXY:
		var req payload.HTTPProxy
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode http_proxy payload: %w", err)
		}
		result, err := rt.HTTPProxy(ctx, req.Method, req.TargetURL, req.Headers, req.Body)
		if err != nil {
			return nil, err
		}

		return marshalOr(*result, nil)

	// ── Network ───────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_LIST_ROUTES:
		var req payload.ListRoutes
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode list_routes payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)
		routes, err := nrt.ListRoutes(ctx, req.LabelSelector)

		return marshalOr(routes, err)

	// ── PVCs / System ─────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_DELETE_PVCS:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_pvcs payload: %w", err)
		}
		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.DeletePVCs(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_GET_SYSTEM_INFO:
		info, err := rt.GetSystemInfo(ctx)

		return marshalOr(info, err)

	case workerpb.CommandType_COMMAND_TYPE_FIND_FREE_SPYRE_CARDS:
		cards, err := helpers.FindFreeSpyreCards(ctx)

		return marshalOr(cards, err)

	case workerpb.CommandType_COMMAND_TYPE_GET_BASE_DIR:
		return marshalOr(utils.GetBaseDir(), nil)

	case workerpb.CommandType_COMMAND_TYPE_RUNTIME_TYPE:
		return marshalOr(rt.Type().String(), nil)

	// ── Helm install / uninstall ──────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_HELM_INSTALL:
		var req payload.HelmInstall
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("helm install: decode payload: %w", err)
		}

		timeout := time.Duration(req.TimeoutSec) * time.Second
		if req.TimeoutSec == 0 {
			timeout = defaultHelmTimeout
		}

		chart, err := helmutil.UnmarshalChart(req.ChartFiles)
		if err != nil {
			return nil, fmt.Errorf("helm install: reconstruct chart for release %q: %w", req.Release, err)
		}

		return nil, helmutil.InstallOrUpgrade(ctx, req.Release, req.Namespace, chart, req.Values, req.TemplateID, timeout)

	case workerpb.CommandType_COMMAND_TYPE_HELM_UNINSTALL:
		var req payload.HelmUninstall
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("helm uninstall: decode payload: %w", err)
		}

		return nil, helmutil.UninstallRelease(ctx, req.Release, req.Namespace)

	case workerpb.CommandType_COMMAND_TYPE_WAIT_INFERENCE_SERVICE:
		var req payload.WaitInferenceService
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("wait_inference_service: decode payload: %w", err)
		}

		oc, ok := rt.(*openshiftRuntime.OpenshiftClient)
		if !ok {
			return nil, fmt.Errorf("wait_inference_service: runtime is not OpenShift")
		}

		return nil, oc.WithNamespace(req.Namespace).WaitForInferenceServiceReady(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_LIST_CRD:
		var req payload.ListCRD
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("list_crd: decode payload: %w", err)
		}

		scoped := rtInNamespace(rt, req.Namespace)

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   req.Group,
			Version: req.Version,
			Kind:    req.Kind,
		})

		filters := map[string][]string{}
		if len(req.LabelKeys) > 0 {
			filters["label"] = req.LabelKeys
		}

		resources, err := scoped.ListCRD(ctx, list, filters)
		if err != nil {
			return nil, err
		}

		wireItems := make([]payload.CRDResource, len(resources))
		for i, r := range resources {
			wireItems[i] = payload.CRDResource{Name: r.Name, Labels: r.Labels}
		}

		return marshalOr(wireItems, nil)

	case workerpb.CommandType_COMMAND_TYPE_DELETE_NAMESPACE:
		var req payload.DeleteNamespace
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("delete_namespace: decode payload: %w", err)
		}

		return nil, rt.DeleteNamespace(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_UPDATE_SECRET:
		var req payload.UpdateSecret
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("update_secret: decode payload: %w", err)
		}

		nrt := rtInNamespace(rt, req.Namespace)

		return nil, nrt.UpdateSecret(ctx, req.Name, req.DeploymentName, req.Data)

	default:
		return nil, fmt.Errorf("unsupported command type: %s", cmd.GetType())
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// rtInNamespace returns the runtime scoped to ns.
// For an OpenShift worker it returns a shallow copy of the existing client with
// only Namespace swapped — no new k8s connections are made.
// For Podman, namespace is not a concept; the runtime is returned unchanged.
// An empty ns is treated as "no scoping needed" and returns rt unchanged.
func rtInNamespace(rt runtime.Runtime, ns string) runtime.Runtime {
	if ns == "" {
		return rt
	}

	if oc, ok := rt.(*openshiftRuntime.OpenshiftClient); ok {
		return oc.WithNamespace(ns)
	}

	// Podman and any other runtime: namespace is not applicable.
	return rt
}

// marshalOr marshals v to JSON, or propagates err if non-nil.
func marshalOr(v any, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}

	return data, nil
}

func okResult(commandID string, data []byte) *workerpb.CommandResult {
	return &workerpb.CommandResult{
		CommandId: commandID,
		Success:   true,
		Data:      data,
	}
}

func failResult(commandID string, err error) *workerpb.CommandResult {
	return &workerpb.CommandResult{
		CommandId: commandID,
		Success:   false,
		Error:     err.Error(),
	}
}
