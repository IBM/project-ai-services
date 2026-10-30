package dispatch

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/httpproxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

const mockBaseDir = "/tmp/ai-services-mock"

// handleMock serves the commands that bypass the runtime.Runtime interface
// (Helm, Spyre discovery, model download, HTTP tunnel, base dir, OpenShift
// InferenceService wait) when rt is the in-memory mock. Nothing real is run.
// handled is false for every other command, which then follows the normal path.
func handleMock(rt runtime.Runtime, cmd *workerpb.Command) (data []byte, handled bool, err error) {
	mc, ok := rt.(*mock.Client)
	if !ok {
		return nil, false, nil
	}

	if data, handled, err := handleMockHelm(mc, cmd); handled {
		return data, true, err
	}

	switch cmd.GetType() {
	case workerpb.CommandType_COMMAND_TYPE_WAIT_INFERENCE_SERVICE,
		workerpb.CommandType_COMMAND_TYPE_DOWNLOAD_MODEL:
		return nil, true, nil

	case workerpb.CommandType_COMMAND_TYPE_FIND_FREE_SPYRE_CARDS:
		data, err := marshalOr(mc.FreeSpyreCards(), nil)

		return data, true, err

	case workerpb.CommandType_COMMAND_TYPE_GET_BASE_DIR:
		data, err := marshalOr(mockBaseDir, nil)

		return data, true, err

	case workerpb.CommandType_COMMAND_TYPE_HTTP_PROXY:
		// Pods are not real, so there is nothing to call; report a healthy 200.
		data, err := marshalOr(httpproxy.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil)

		return data, true, err

	default:
		return nil, false, nil
	}
}

// handleMockHelm records Helm releases in memory instead of talking to a cluster.
func handleMockHelm(mc *mock.Client, cmd *workerpb.Command) (data []byte, handled bool, err error) {
	p := cmd.GetPayload()

	switch cmd.GetType() {
	case workerpb.CommandType_COMMAND_TYPE_HELM_INSTALL:
		var req payload.HelmInstall
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, true, fmt.Errorf("helm install: decode payload: %w", err)
		}

		mc.InstallRelease(req.Namespace, req.Release, req.TemplateID)

		return nil, true, nil

	case workerpb.CommandType_COMMAND_TYPE_HELM_UNINSTALL:
		var req payload.HelmRelease
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, true, fmt.Errorf("helm uninstall: decode payload: %w", err)
		}

		mc.UninstallRelease(req.Namespace, req.Release)

		return nil, true, nil

	case workerpb.CommandType_COMMAND_TYPE_HELM_GET_MANIFEST:
		var req payload.HelmRelease
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, true, fmt.Errorf("helm_get_manifest: decode payload: %w", err)
		}

		if !mc.ReleaseExists(req.Namespace, req.Release) {
			return nil, true, fmt.Errorf("release %q not found in namespace %q", req.Release, req.Namespace)
		}

		data, err := marshalOr(payload.HelmManifest{}, nil)

		return data, true, err

	default:
		return nil, false, nil
	}
}
