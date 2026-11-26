// Package keys manages the LiteLLM virtual keys that give callers access to a model: the public
// keys handed to users, and the internal keys of the applications that use a model.
package keys

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Manager generates LiteLLM keys.
type Manager struct {
	litellm catalogclient.LiteLLMClient
}

// NewManager creates a key manager. litellm creates and revokes the keys.
func NewManager(litellm catalogclient.LiteLLMClient) *Manager {
	return &Manager{litellm: litellm}
}

// Request describes the key to generate. The caller decides what kind of key it is by the alias:
// litellm.PublicKeyAlias for the public key of a model, litellm.InternalKeyAlias for the key an
// application reaches it with.
type Request struct {
	// Model is the name the key may call. It must not be empty.
	Model string
	// ID is the component or connector ID: the tag the model's LiteLLM route was registered with.
	// The key is pinned to it, so it reaches only that deployment and not other deployments of the
	// same model name.
	ID uuid.UUID
	// Alias names the key, and LiteLLM keeps it unique.
	Alias string
}

// Generate generates the LiteLLM key described by req and returns it.
//
// The caller must already have checked that the requester may use the model: Generate does not
// check ownership. Any existing key with the alias is revoked first, so at most one key exists per
// alias and repeating a request does not fail because the alias is taken. The key is returned once
// and is neither stored nor logged.
//
// Concurrent requests for one alias are not serialised: if their revoke and generate calls
// interleave, LiteLLM rejects the second key because its alias exists, and that request fails with
// a 502 asking the caller to retry. There is still never more than one key.
//
// Errors of type *validators.ValidationError carry the HTTP status to return; any other error is
// an internal failure.
func (m *Manager) Generate(ctx context.Context, req Request) (string, error) {
	if req.Model == "" {
		return "", fmt.Errorf("%s has no model name: cannot generate its key", req.ID)
	}

	// Revoking first is what keeps the number of keys per alias at one. A "not found" answer is
	// handled by the client: it only means no key existed yet.
	if err := m.litellm.DeleteKeysByAlias(ctx, []string{req.Alias}); err != nil {
		logger.ErrorfCtx(ctx, "failed to revoke key %q: %v", req.Alias, err)

		return "", validators.BadGateway("LiteLLM gateway rejected or could not process the key revocation; the existing key was not changed")
	}

	key, err := m.litellm.GenerateKey(ctx, catalogclient.LiteLLMKeyGenerateRequest{
		KeyAlias: req.Alias,
		Models:   []string{req.Model},
		Metadata: catalogclient.LiteLLMKeyMetadata{Tags: []string{req.ID.String()}},
	})
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to generate key %q: %v", req.Alias, err)

		// Any old key was already deleted above, so no key exists now and the caller must retry.
		return "", validators.BadGateway("LiteLLM gateway failed to generate the key; no key exists for this model right now: retry the request")
	}

	return key, nil
}

// Revoke revokes the keys with the given aliases. An alias that has no key is not an error, so
// revoking twice is safe. A failure is a *validators.ValidationError (502) and leaves the keys
// that could not be revoked in place.
func (m *Manager) Revoke(ctx context.Context, aliases []string) error {
	if len(aliases) == 0 {
		return nil
	}

	if err := m.litellm.DeleteKeysByAlias(ctx, aliases); err != nil {
		logger.ErrorfCtx(ctx, "failed to revoke keys %q: %v", aliases, err)

		return validators.BadGateway("LiteLLM gateway rejected or could not process the key revocation")
	}

	return nil
}
