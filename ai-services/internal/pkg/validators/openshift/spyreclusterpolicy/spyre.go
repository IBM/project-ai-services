package spyrepolicy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	spyreGroup   = "spyre.ibm.com"
	spyreVersion = "v1alpha1"
	spyreKind    = "SpyreClusterPolicy"
	spyreName    = "spyreclusterpolicy"
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

// Verify performs a direct fetch.
func (r *SpyrePolicyRule) Verify() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   spyreGroup,
		Version: spyreVersion,
		Kind:    spyreKind,
	})

	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorpPollInterval, constants.OperatorpPollTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Client.Get(ctx, types.NamespacedName{
			Name:      spyreName,
			Namespace: constants.SpyreOperatorNamespace,
		}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Infof("SpyreClusterPolicy %s not found yet, retrying...", spyreName, logger.VerbosityLevelDebug)

				return false, nil
			}

			return false, fmt.Errorf("failed to find %s in namespace %s: %w", spyreName, constants.SpyreOperatorNamespace, err)
		}

		state, found, err := unstructured.NestedString(obj.Object, "status", "state")
		if err != nil {
			return false, fmt.Errorf("failed to parse status.state from policy: %w", err)
		}

		if !found || state != "ready" {
			if !found {
				state = "unknown"
			}
			logger.Infof("SpyreClusterPolicy not ready yet (status.state: %s), waiting...", state, logger.VerbosityLevelDebug)

			return false, nil
		}
		logger.Infof("SpyreClusterPolicy %s is ready", spyreName, logger.VerbosityLevelDebug)

		return true, nil
	})
}

func (r *SpyrePolicyRule) Message() string {
	return "Spyre Cluster Policy is ready"
}

func (r *SpyrePolicyRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *SpyrePolicyRule) Hint() string {
	return fmt.Sprintf("Run 'oc get spyreclusterpolicy -n %s' and ensure status.state is 'ready'.", constants.SpyreOperatorNamespace)
}
