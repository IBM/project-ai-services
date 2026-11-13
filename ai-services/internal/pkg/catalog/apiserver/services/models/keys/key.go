// Package keys manages the LiteLLM virtual keys that give callers outside the platform access to a
// model.
package keys

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// publicKeyType is the type reported in the response for a public virtual key.
	publicKeyType = "public"
)

// Manager generates the public LiteLLM keys of models.
type Manager struct {
	litellm catalogclient.LiteLLMClient
}

// NewManager creates a key manager. litellm creates and revokes the keys.
func NewManager(litellm catalogclient.LiteLLMClient) *Manager {
	return &Manager{litellm: litellm}
}

// Generate generates the public LiteLLM key of a model, local or remote.
//
// The model must already be one the caller may use: Generate does not check ownership. The key is
// aliased "<modelName>-<id>", which LiteLLM keeps unique. Any existing key with that alias is revoked first, so
// at most one public key exists per ID. The key is returned once and is neither stored nor logged.
//
// Concurrent requests for one ID are not serialised: if their revoke and generate calls interleave,
// LiteLLM rejects the second key because its alias exists, and that request fails with a 502 asking
// the caller to retry. There is still never more than one key.
//
// Errors of type *validators.ValidationError carry the HTTP status to return; any other error is
// an internal failure.
func (m *Manager) Generate(ctx context.Context, model *dbrepo.ModelRow) (*apimodels.GenerateModelKeyResponse, error) {
	if model.Model == "" {
		return nil, fmt.Errorf("%s has no model name in its metadata", model.ID)
	}

	alias := fmt.Sprintf("%s-%s", model.Model, model.ID)

	key, err := m.rotatePublicKey(ctx, model.ID, alias, model.Model)
	if err != nil {
		return nil, err
	}

	return &apimodels.GenerateModelKeyResponse{Key: key, Type: publicKeyType, Model: model.Model}, nil
}

// rotatePublicKey revokes the key with the given alias, if any, and generates a new one.
func (m *Manager) rotatePublicKey(ctx context.Context, id uuid.UUID, alias, model string) (string, error) {
	// Revoking first is what keeps the number of public keys at one. A "not found" answer is
	// handled by the client: it only means no key existed yet.
	if err := m.litellm.DeleteKeysByAlias(ctx, []string{alias}); err != nil {
		logger.ErrorfCtx(ctx, "failed to revoke public key %q: %v", alias, err)

		return "", validators.BadGateway("LiteLLM gateway rejected or could not process the key revocation; the existing public key was not changed")
	}

	key, err := m.litellm.GenerateKey(ctx, catalogclient.LiteLLMKeyGenerateRequest{
		KeyAlias: alias,
		Models:   []string{model},
		// The ID is the tag the model's LiteLLM route was registered with, so the key reaches
		// only that deployment and not other deployments of the same model name.
		Metadata: catalogclient.LiteLLMKeyMetadata{Tags: []string{id.String()}},
	})
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to generate public key %q: %v", alias, err)

		// Any old key was already deleted above, so no key exists now and the caller must retry.
		return "", validators.BadGateway("LiteLLM gateway failed to generate the key; no public key exists for this model right now: call this endpoint again")
	}

	return key, nil
}
