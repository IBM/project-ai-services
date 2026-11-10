// Package keys manages the LiteLLM virtual keys that give callers outside the platform access to a
// model.
package keys

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	litellm       catalogclient.LiteLLMClient
	locks         aliasLocks
}

// NewManager creates a key manager. The repositories resolve a model ID to its model name and
// owner; litellm creates and revokes the keys.
func NewManager(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	litellm catalogclient.LiteLLMClient,
) *Manager {
	return &Manager{componentRepo: componentRepo, connectorRepo: connectorRepo, litellm: litellm}
}

// aliasLocks serialises work per key alias, so two concurrent key requests for one model ID cannot
// interleave their revoke and generate calls. It is per process: the catalog API runs as a single
// instance, and LiteLLM's alias uniqueness still rejects a second key if that ever changes.
type aliasLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// lock blocks until the lock for name is held and returns the function that releases it.
func (m *aliasLocks) lock(name string) func() {
	m.mu.Lock()
	l, ok := m.locks[name]
	if !ok {
		if m.locks == nil {
			m.locks = map[string]*sync.Mutex{}
		}
		l = &sync.Mutex{}
		m.locks[name] = l
	}
	m.mu.Unlock()

	l.Lock()

	return l.Unlock
}

// Generate generates the public LiteLLM key of the model identified by id.
//
// id is the ID of a model (a component or a connector). The key is aliased "<modelName>-<id>",
// which LiteLLM keeps unique. Any existing key with that alias is revoked first, so at most one public key exists per ID. The key is returned once and is
// neither stored nor logged.
//
// Errors of type *validators.ValidationError carry the HTTP status to return; any other error is
// an internal failure.
func (m *Manager) Generate(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.GenerateModelKeyResponse, error) {
	// TODO Reuse the method to fetch model once list is merged
	model, err := m.modelNameOf(ctx, id, createdBy)
	if err != nil {
		return nil, err
	}

	alias := fmt.Sprintf("%s-%s", model, id)

	// The lock covers the revoke and the generate, so a concurrent request cannot slip a key in
	// between them.
	defer m.locks.lock(alias)()

	key, err := m.rotatePublicKey(ctx, alias, model)
	if err != nil {
		return nil, err
	}

	return &apimodels.GenerateModelKeyResponse{Key: key, Type: publicKeyType, Model: model}, nil
}

// rotatePublicKey revokes the key with the given alias, if any, and generates a new one.
func (m *Manager) rotatePublicKey(ctx context.Context, alias, model string) (string, error) {
	// Revoking first is what keeps the number of public keys at one. A "not found" answer is
	// handled by the client: it only means no key existed yet.
	if err := m.litellm.DeleteKeysByAlias(ctx, []string{alias}); err != nil {
		logger.ErrorfCtx(ctx, "failed to revoke public key %q: %v", alias, err)

		return "", validators.BadGateway("LiteLLM gateway rejected or could not process the key revocation; the existing public key was not changed")
	}

	key, err := m.litellm.GenerateKey(ctx, catalogclient.LiteLLMKeyGenerateRequest{
		KeyAlias: alias,
		Models:   []string{model},
	})
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to generate public key %q: %v", alias, err)

		// Any old key was already deleted above, so no key exists now and the caller must retry.
		return "", validators.BadGateway("LiteLLM gateway failed to generate the key; no public key exists for this model right now: call this endpoint again")
	}

	return key, nil
}

// modelNameOf resolves a component or connector ID to the name of the model it serves. The ID is
// looked up in components first, then in connectors; 404 when it is in neither, 403 when the row
// was created by someone other than userID.
func (m *Manager) modelNameOf(ctx context.Context, id uuid.UUID, userID string) (string, error) {
	component, err := m.componentRepo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get component: %w", err)
	}
	if component != nil {
		if err := ensureOwner(component.CreatedBy, userID); err != nil {
			return "", err
		}

		return modelFromMetadata(component.Metadata, id)
	}

	connector, err := m.connectorRepo.GetByID(ctx, id, true)
	if errors.Is(err, dbrepo.ErrConnectorNotFound) {
		return "", validators.NotFound("model %q not found", id)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get connector: %w", err)
	}
	if connector.Type != catalogconstants.ConnectorTypeModels {
		return "", validators.NotFound("model %q not found", id)
	}
	if err := ensureOwner(connector.CreatedBy, userID); err != nil {
		return "", err
	}

	return modelFromMetadata(connector.Metadata, id)
}

// ensureOwner returns a 403 when the row was created by someone other than userID. An empty
// userID is not checked.
func ensureOwner(createdBy, userID string) error {
	if userID != "" && createdBy != userID {
		return validators.Forbidden("user does not own this model")
	}

	return nil
}

func modelFromMetadata(metadata map[string]any, id uuid.UUID) (string, error) {
	model, _ := metadata[catalogconstants.MetadataKeyModel].(string)
	if model == "" {
		return "", fmt.Errorf("%s has no model name in its metadata", id)
	}

	return model, nil
}
