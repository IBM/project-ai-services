package spyrepolicy

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	return "scp"
}

func (r *SpyrePolicyRule) Description() string {
	return "Validates that Spyre Cluster Policy is in ready state"
}

// Verify performs a direct check without polling.
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

	if err := client.Client.Get(client.Ctx, types.NamespacedName{Name: spyreName}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("SpyreClusterPolicy %s not found", spyreName)
		}

		return fmt.Errorf("failed to find %s: %w", spyreName, err)
	}

	state, found, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return fmt.Errorf("failed to parse status.state from policy: %w", err)
	}

	if !found {
		return fmt.Errorf("SpyreClusterPolicy status.state not found")
	}

	if state != "ready" {
		return fmt.Errorf("SpyreClusterPolicy not ready (status.state: %s)", state)
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
	return "Run 'oc get spyreclusterpolicy and ensure status.state is 'ready'."
}
