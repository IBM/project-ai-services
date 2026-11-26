package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/credentialspec"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// credentialSecretKey is the key inside the Secret that holds the credential. The service
// reads it from a file mounted at <mount>/credential.
const credentialSecretKey = "credential"

// SecretStore is the part of the runtime that stores a credential Secret.
type SecretStore interface {
	CreateSecret(ctx context.Context, name string, data map[string][]byte, labels map[string]string) error
	DeleteSecret(ctx context.Context, name string) error
}

// CredentialSecrets are the Secrets created for one service deployment.
type CredentialSecrets struct {
	deploymentID string
	names        []string
	p            CredentialProvisioner
	store        SecretStore
}

// Names returns the names of the Secrets that were created.
func (c *CredentialSecrets) Names() []string {
	if c == nil {
		return nil
	}

	return c.names
}

// Rollback removes what ProvisionCredentialSecrets created: it revokes the deployment's
// credentials and deletes the Secrets. Call it when the deployment failed after provisioning.
// It is safe on nil, and a failure is only logged so it never hides the error that caused it.
func (c *CredentialSecrets) Rollback(ctx context.Context) {
	if c == nil {
		return
	}
	if err := c.p.RevokeForDeployment(ctx, c.deploymentID); err != nil {
		logger.WarningfCtx(ctx, "Could not revoke service credentials of failed deployment %s: %v\n", c.deploymentID, err)
	}
	for _, name := range c.names {
		if err := c.store.DeleteSecret(ctx, name); err != nil {
			logger.WarningfCtx(ctx, "Could not delete credential secret %s of failed deployment %s: %v\n", name, c.deploymentID, err)
		}
	}
}

// ProvisionCredentialSecrets creates, for each @credential annotation of a service, a new
// credential with the annotated grants and stores it in a Secret. It then sets the annotated
// values key to the name of that Secret. The credential itself never enters values, so it
// reaches neither the Helm release nor the pod spec.
//
// The caller of the credential is always serviceID, the service being deployed: an
// annotation cannot name another caller. Specs must come from a built-in asset; the catalog
// provider refuses bundles. The value of a credential is never logged.
//
// A nil provisioner or store leaves values unchanged and creates nothing, so a deployment
// without credential support behaves as before. On an error nothing is left behind.
func ProvisionCredentialSecrets(
	ctx context.Context,
	specs []credentialspec.Spec,
	serviceID, deploymentID string,
	values map[string]any,
	p CredentialProvisioner,
	store SecretStore,
) (*CredentialSecrets, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if p == nil || store == nil {
		logger.WarningfCtx(ctx, "Service %s declares a service credential, but credentials are not available; deploying without it\n", serviceID)

		return nil, nil
	}
	if values == nil {
		return nil, fmt.Errorf("service %s: no values to receive the credential secret name", serviceID)
	}

	out := &CredentialSecrets{deploymentID: deploymentID, p: p, store: store}
	for _, spec := range specs {
		if err := provisionOne(ctx, out, spec, serviceID, values); err != nil {
			out.Rollback(ctx)

			return nil, fmt.Errorf("failed to provision service credential for %s: %w", serviceID, err)
		}
	}

	return out, nil
}

func provisionOne(ctx context.Context, out *CredentialSecrets, spec credentialspec.Spec, serviceID string, values map[string]any) error {
	if _, declared := values[spec.ValuesKey]; !declared {
		return fmt.Errorf("values key %q is not declared by the service", spec.ValuesKey)
	}

	credential, err := out.p.ProvisionWithGrants(ctx, serviceID, out.deploymentID, spec.Grants)
	if err != nil {
		return err
	}

	name := credentialSecretName(serviceID, out.deploymentID, spec.ValuesKey)
	labels := map[string]string{constants.ApplicationTemplateKey: out.deploymentID}
	if err := out.store.CreateSecret(ctx, name, map[string][]byte{credentialSecretKey: []byte(credential)}, labels); err != nil {
		return errors.New("could not store the credential secret")
	}

	out.names = append(out.names, name)
	// Always overwrite: a value supplied in the request must not point the pod at another Secret.
	values[spec.ValuesKey] = name

	return nil
}

// credentialSecretName is unique per deployment and values key, and is a valid Kubernetes
// and Podman secret name for a catalog service ID.
func credentialSecretName(serviceID, deploymentID, valuesKey string) string {
	return fmt.Sprintf("%s-credential-%s", serviceID, catalogutils.GenerateInstanceSlug(deploymentID+":"+valuesKey))
}
