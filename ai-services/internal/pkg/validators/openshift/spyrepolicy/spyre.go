package spyrepolicy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	return "spyre-cluster-policy"
}

func (r *SpyrePolicyRule) Description() string {
	return "Validates that Spyre Cluster Policy is in ready state"
}

// Verify performs a direct fetch.
func (r *SpyrePolicyRule) Verify() error {
	ctx := context.Background()

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

	err = client.Client.Get(ctx, types.NamespacedName{
		Name:      spyreName,
		Namespace: constants.SpyreOperatorNamespace,
	}, obj)
	if err != nil {
		return fmt.Errorf("failed to find %s in namespace %s: %w", spyreName, constants.SpyreOperatorNamespace, err)
	}

	state, found, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return fmt.Errorf("failed to parse status.state from policy: %w", err)
	}

	if !found || state != "ready" {
		if !found {
			state = "unknown"
		}

		return fmt.Errorf("spyre cluster policy is not ready (current state: %s)", state)
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
	return fmt.Sprintf("Run 'oc get spyreclusterpolicy -n %s' and ensure status.state is 'ready'.", constants.SpyreOperatorNamespace)
}
