package adminaccess

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"

	authv1 "k8s.io/api/authorization/v1"
)

const (
	operatorGroup     = "operators.coreos.com"
	operatorResource  = "subscriptions"
	operatorVerb      = "create"
	operatorNamespace = "openshift-operators"
)

type OperatorPermissionRule struct{}

func NewOperatorPermissionRule() *OperatorPermissionRule {
	return &OperatorPermissionRule{}
}

func (r *OperatorPermissionRule) Name() string {
	return "operator-permission"
}

func (r *OperatorPermissionRule) Description() string {
	return "Validates that the current user has permission to install operators"
}

func (r *OperatorPermissionRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("create openshift client: %w", err)
	}

	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:     operatorGroup,
				Resource:  operatorResource,
				Verb:      operatorVerb,
				Namespace: operatorNamespace,
			},
		},
	}

	if err := client.Client.Create(ctx, review); err != nil {
		return fmt.Errorf("validate operator installation permission: %w", err)
	}

	if !review.Status.Allowed {
		return fmt.Errorf("user does not have permission to install operators")
	}

	return nil
}

func (r *OperatorPermissionRule) Message() string {
	return "Operator installation permission validated"
}

func (r *OperatorPermissionRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *OperatorPermissionRule) Hint() string {
	return "Ensure your kubeconfig user has cluster-admin or sufficient RBAC permissions to install operators."
}
