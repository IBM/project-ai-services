// Package uninstall removes all worker-node components deployed by
// `worker join`: the Caddy reverse-proxy pod and the on-disk worker data
// directory (Caddyfile, caddy state, etc.).
//
// It is intentionally scoped to what `worker join` created.  Application pods
// deployed on the worker by the catalog are the operator's responsibility and
// are not touched here.
package uninstall

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/openshift"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/podman"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

// Uninstall removes all worker components deployed by `worker join`.
func Uninstall(ctx context.Context, opts workerutils.UninstallOptions) error {
	switch opts.RuntimeType {
	case types.RuntimeTypePodman:
		return workerpodman.Uninstall(ctx, opts)
	case types.RuntimeTypeOpenShift:
		return workeropenshift.Uninstall(ctx, opts)
	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.RuntimeType)
	}
}
