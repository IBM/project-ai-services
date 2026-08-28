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

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
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
		pods, err := rt.ListPods(ctx, req.Filters)

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

		return nil, rt.StopPod(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_START_POD:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode start_pod payload: %w", err)
		}

		return nil, rt.StartPod(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_INSPECT_POD:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode inspect_pod payload: %w", err)
		}
		pod, err := rt.InspectPod(ctx, req.NameOrID)

		return marshalOr(pod, err)

	case workerpb.CommandType_COMMAND_TYPE_POD_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode pod_exists payload: %w", err)
		}
		exists, err := rt.PodExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	case workerpb.CommandType_COMMAND_TYPE_POD_LOGS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode pod_logs payload: %w", err)
		}

		return nil, rt.PodLogs(ctx, req.NameOrID)

	case workerpb.CommandType_COMMAND_TYPE_GET_POD_RESOURCES:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode get_pod_resources payload: %w", err)
		}
		pr, err := rt.GetPodResources(ctx, req.NameOrID)

		return marshalOr(pr, err)

	// ── Secrets ───────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_LIST_SECRETS:
		var req payload.ListSecrets
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode list_secrets payload: %w", err)
		}
		names, err := rt.ListSecrets(ctx, req.Filters)

		return marshalOr(names, err)

	case workerpb.CommandType_COMMAND_TYPE_DELETE_SECRET:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_secret payload: %w", err)
		}

		return nil, rt.DeleteSecret(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_SECRET_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode secret_exists payload: %w", err)
		}
		exists, err := rt.SecretExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	// ── Volumes ───────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_DELETE_VOLUME:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_volume payload: %w", err)
		}

		return nil, rt.DeleteVolume(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_VOLUME_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode volume_exists payload: %w", err)
		}
		exists, err := rt.VolumeExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	// ── Containers ────────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_INSPECT_CONTAINER:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode inspect_container payload: %w", err)
		}
		c, err := rt.InspectContainer(ctx, req.NameOrID)

		return marshalOr(c, err)

	case workerpb.CommandType_COMMAND_TYPE_CONTAINER_EXISTS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode container_exists payload: %w", err)
		}
		exists, err := rt.ContainerExists(ctx, req.NameOrID)

		return marshalOr(exists, err)

	case workerpb.CommandType_COMMAND_TYPE_CONTAINER_LOGS:
		var req payload.NameOrID
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode container_logs payload: %w", err)
		}

		return nil, rt.ContainerLogs(ctx, req.NameOrID)

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
		return nil, helpers.DownloadModelContainer(ctx, req.Model, req.TargetDir)

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
		routes, err := rt.ListRoutes(ctx, req.LabelSelector)

		return marshalOr(routes, err)

	// ── PVCs / System ─────────────────────────────────────────────────────────

	case workerpb.CommandType_COMMAND_TYPE_DELETE_PVCS:
		var req payload.Name
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, fmt.Errorf("decode delete_pvcs payload: %w", err)
		}

		return nil, rt.DeletePVCs(ctx, req.Name)

	case workerpb.CommandType_COMMAND_TYPE_GET_SYSTEM_INFO:
		info, err := rt.GetSystemInfo(ctx)

		return marshalOr(info, err)

	case workerpb.CommandType_COMMAND_TYPE_RUNTIME_TYPE:
		return marshalOr(rt.Type().String(), nil)

	default:
		return nil, fmt.Errorf("unsupported command type: %s", cmd.GetType())
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

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
