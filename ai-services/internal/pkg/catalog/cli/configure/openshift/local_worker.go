package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/openshift"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

// JoinAsLocalWorker deploys the worker on OpenShift and connects it to the
// catalog-backend as the "Local" worker.
//
// The catalog-backend gateway skips ValidateToken when LOCAL_WORKER=true, so
// the sentinel LocalWorkerToken is used and no token needs to live in any TokenStore.
func JoinAsLocalWorker(ctx context.Context) error {
	logger.InfolnCtx(ctx, "Joining this machine as the Local worker...")

	gatewayAddr := fmt.Sprintf("%s:%d", workerconstants.OpenShiftGatewayServiceEndpoint, workerconstants.WorkerGatewayPort)

	opts := workertypes.OpenshiftWorkerOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       workerconstants.LocalWorkerToken,
		},
	}

	if err := workeropenshift.DeployWorker(ctx, opts); err != nil {
		return fmt.Errorf("local worker join: deploy worker: %w", err)
	}

	logger.InfolnCtx(ctx, "Local worker joined successfully.")

	return nil
}
