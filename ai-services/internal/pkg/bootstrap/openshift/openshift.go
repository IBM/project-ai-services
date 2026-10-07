package openshift

import "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"

// upgradeMode controls whether existing operator subscriptions are offered an
// upgrade prompt instead of being silently skipped.
var upgradeMode bool

// SetUpgradeMode enables or disables the operator upgrade prompt flow.
func SetUpgradeMode(upgrade bool) {
	upgradeMode = upgrade
}

// OpenshiftBootstrap implements Bootstrap interface for Openshift runtime.
type OpenshiftBootstrap struct{}

// NewOpenshiftBootstrap creates a new Podman Openshift instance.
func NewOpenshiftBootstrap() *OpenshiftBootstrap {
	return &OpenshiftBootstrap{}
}

// Type returns the runtime type.
func (o *OpenshiftBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}
