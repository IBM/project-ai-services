package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

const (
	spyrePollInterval = 10 * time.Second
)

type OCPHelper struct {
	dynamicClient dynamic.Interface
}

func NewOCPHelper() (*OCPHelper, error) {
	cfg, err := openshift.GetKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kube config: %w", err)
	}

	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &OCPHelper{
		dynamicClient: dc,
	}, nil
}

/* -------- SpyreClusterPolicy -------- */

// WaitForSpyreClusterPolicyReady waits until the Spyre Cluster Policy is in ready state or timeout occurs.
func (h *OCPHelper) WaitForSpyreClusterPolicyReady(
	ctx context.Context,
	name string,
	timeout time.Duration,
) error {
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
			obj, err := h.dynamicClient.Resource(gvr).Get(ctx, name, v1.GetOptions{})
			if err != nil {
				// Resource might not be created yet
				return false, nil
			}

			// Check .status.state for "ready"
			state, found, _ := unstructured.NestedString(
				obj.Object, "status", "state",
			)

			return found && state == "ready", nil
		},
	)
}
