package spyrepolicy

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	spyreGroup    = "spyre.ibm.com"
	spyreVersion  = "v1alpha1"
	spyreKind     = "SpyreClusterPolicy"
	spyreTimeout  = 10 * time.Minute
	spyreInterval = 10 * time.Second
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

// Verify checks if the Spyre Cluster Policy is ready.
func (r *SpyrePolicyRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	if err := waitForSpyreReady(ctx, client, "spyreclusterpolicy", spyreTimeout); err != nil {
		return fmt.Errorf("spyre cluster policy is not ready: %w", err)
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
	return "Run 'oc get spyreclusterpolicy' and ensure status.state is 'ready'."
}

// waitForSpyreReady polls until the SpyreClusterPolicy is in ready state.
func waitForSpyreReady(ctx context.Context, client *openshift.OpenshiftClient, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, spyreInterval, timeout, true, func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   spyreGroup,
			Version: spyreVersion,
			Kind:    spyreKind,
		})

		err := client.Client.Get(ctx, types.NamespacedName{Name: name}, obj)
		if err != nil {
			return false, nil
		}

		state, found, _ := unstructured.NestedString(obj.Object, "status", "state")

		return found && state == "ready", nil
	})
}
