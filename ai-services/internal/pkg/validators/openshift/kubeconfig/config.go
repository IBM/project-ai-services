package kubeconfig

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// Check if the current user has permission to create namespaces
	sar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:     "create",
				Group:    "",
				Resource: "namespaces",
			},
		},
	}

	result, err := client.KubeClient.AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to check namespace creation permissions: %w", err)
	}

	if !result.Status.Allowed {
		reason := "insufficient permissions"
		if result.Status.Reason != "" {
			reason = result.Status.Reason
		}

		return fmt.Errorf("user does not have permission to create namespaces: %s", reason)
	}

	return nil
}

func (r *KubeconfigRule) Message() string {
	return "Cluster authentication successful"
}

func (r *KubeconfigRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelCritical
}

func (r *KubeconfigRule) Hint() string {
	return "Make sure your kubeconfig is correctly configured and that you have the necessary permissions to access the OpenShift cluster."
}
