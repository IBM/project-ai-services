package spyrepolicy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

type SpyrePolicyRule struct{}

func NewSpyrePolicyRule() *SpyrePolicyRule {
	return &SpyrePolicyRule{}
}

func (r *SpyrePolicyRule) Name() string {
	return "scp"
}

func (r *SpyrePolicyRule) Description() string {
	return "Validates that Spyre Cluster Policy is in ready state"
}

// Verify performs a direct check without polling.
func (r *SpyrePolicyRule) Verify(ctx context.Context) error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	state, found, err := utils.GetSpyreClusterPolicyState(ctx, client)
	if err != nil {
		return err
	}

	if !found || (state != utils.SpyreStateReady && state != utils.SpyreStateNoSpyreNodes) {
		return fmt.Errorf("SpyreClusterPolicy not ready (status.state: %q)", state)
	}

	return nil
}

func (r *SpyrePolicyRule) Message() string {
	return "Spyre Cluster Policy is ready"
}

func (r *SpyrePolicyRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *SpyrePolicyRule) Hint() string {
	return "Run 'oc get spyreclusterpolicy' and ensure status.state is 'ready' or 'no Spyre nodes'."
}
