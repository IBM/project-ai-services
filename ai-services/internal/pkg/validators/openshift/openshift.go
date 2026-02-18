package openshift

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"

	runtimeOpenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
)

const (
	spyrePollInterval = 10 * time.Second
)

// ValidateSpyreClusterPolicy validates that Spyre Cluster Policy is in ready state.
func ValidateSpyreClusterPolicy(ctx context.Context, timeout time.Duration) error {
	cfg, err := runtimeOpenshift.GetKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to get kube config: %w", err)
	}

	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "spyre.ibm.com",
		Version:  "v1alpha1",
		Resource: "spyreclusterpolicies",
	}

	return wait.PollUntilContextTimeout(
		ctx,
		spyrePollInterval,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			obj, err := dc.Resource(gvr).Get(ctx, "spyreclusterpolicy", v1.GetOptions{})
			if err != nil {
				return false, nil
			}

			state, found, _ := unstructured.NestedString(
				obj.Object, "status", "state",
			)

			return found && state == "ready", nil
		},
	)
}
