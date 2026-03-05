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

// SpyrePolicyRule validates that the Spyre Cluster Policy is installed and ready.
type SpyrePolicyRule struct {
	client *openshift.OpenshiftClient
}

func NewSpyrePolicyRule(client *openshift.OpenshiftClient) *SpyrePolicyRule {
	return &SpyrePolicyRule{
		client: client,
	}
}

func (r *SpyrePolicyRule) Name() string {
	return "spyre-cluster-policy"
}

func (r *SpyrePolicyRule) Description() string {
	return "Validates that Spyre Cluster Policy is in ready state"
}

func (r *SpyrePolicyRule) Verify() error {
	if r.client == nil {
		return fmt.Errorf("openshift client is not initialized")
	}
	ctx := r.client.Ctx

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   spyreGroup,
		Version: spyreVersion,
		Kind:    spyreKind,
	})

	return wait.PollUntilContextTimeout(ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true,
		func(ctx context.Context) (bool, error) {
			err := r.client.Client.Get(ctx, types.NamespacedName{
				Name:      spyreName,
				Namespace: constants.SpyreOperatorNamespace,
			}, obj)
			if err != nil {
				if apierrors.IsNotFound(err) {
					logger.Infof("SpyreClusterPolicy %s not found yet, retrying...", spyreName, logger.VerbosityLevelDebug)

					return false, nil
				}

				return false, fmt.Errorf("failed to fetch SpyreClusterPolicy %s: %w", spyreName, err)
			}

			state, found, err := unstructured.NestedString(obj.Object, "status", "state")
			if err != nil {
				return false, fmt.Errorf("failed to parse status.state from SpyreClusterPolicy: %w", err)
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
