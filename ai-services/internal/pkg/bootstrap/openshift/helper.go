package openshift

import (
	"context"
	"time"

	kubeconfig "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	spyrePollInterval = 10 * time.Second
)

type OCPHelper struct {
	KubeClient    *kubernetes.Clientset
	DynamicClient dynamic.Interface
}

func NewOCPHelper() (*OCPHelper, error) {
	cfg, err := kubeconfig.GetKubeConfig()
	if err != nil {
		return nil, err
	}

	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &OCPHelper{
		KubeClient:    kc,
		DynamicClient: dc,
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
			obj, err := h.DynamicClient.Resource(gvr).Get(ctx, name, v1.GetOptions{})
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
