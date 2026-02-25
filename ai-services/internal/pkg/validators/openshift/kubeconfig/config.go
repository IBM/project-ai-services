package kubeconfig

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	corev1 "k8s.io/api/core/v1"
)

type KubeconfigRule struct{}

func NewKubeconfigRule() *KubeconfigRule {
	return &KubeconfigRule{}
}

func (r *KubeconfigRule) Name() string {
	return "kubeconfig"
}

func (r *KubeconfigRule) Description() string {
	return "Validates that kubeconfig can access the OpenShift cluster"
}

// Verify checks if the kubeconfig can access the OpenShift cluster.
func (r *KubeconfigRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// listing namespaces to validate cluster access.
	if err := client.Client.List(ctx, &corev1.NamespaceList{}); err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	return nil
}

func (r *KubeconfigRule) Message() string {
	return "Cluster authentication successful"
}

func (r *KubeconfigRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *KubeconfigRule) Hint() string {
	return "Make sure your kubeconfig is correctly configured and that you have the necessary permissions to access the OpenShift cluster."
}
