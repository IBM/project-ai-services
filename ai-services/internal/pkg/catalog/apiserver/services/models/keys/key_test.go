package keys

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const keyTestModel = "ibm-granite/granite-3.3-8b-instruct"

// keyLiteLLM records the order of key calls and keeps at most one key per alias, like LiteLLM.
type keyLiteLLM struct {
	catalogclient.LiteLLMClient
	mu        sync.Mutex
	calls     []string
	keys      map[string]string
	deleteErr error
	genErr    error
	genReq    catalogclient.LiteLLMKeyGenerateRequest
	deleted   []string
}

func (l *keyLiteLLM) DeleteKeysByAlias(_ context.Context, aliases []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, "delete")
	if l.deleteErr != nil {
		return l.deleteErr
	}
	l.deleted = append(l.deleted, aliases...)
	for _, a := range aliases {
		delete(l.keys, a)
	}

	return nil
}

func (l *keyLiteLLM) GenerateKey(_ context.Context, req catalogclient.LiteLLMKeyGenerateRequest) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, "generate")
	if l.genErr != nil {
		return "", l.genErr
	}
	if _, exists := l.keys[req.KeyAlias]; exists {
		return "", errors.New("alias already exists")
	}

	key := "sk-" + uuid.NewString()
	l.keys[req.KeyAlias] = key
	l.genReq = req

	return key, nil
}

func (l *keyLiteLLM) GetModelInfo(context.Context, string) (*catalogclient.LiteLLMModelInfoResponse, error) {
	return &catalogclient.LiteLLMModelInfoResponse{}, nil
}

func newKeyManager(lite *keyLiteLLM) *Manager {
	if lite.keys == nil {
		lite.keys = map[string]string{}
	}

	return NewManager(lite)
}

func requireBadGateway(t *testing.T, err error) *validators.ValidationError {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadGateway, valErr.Code)

	return valErr
}

func TestGenerate(t *testing.T) {
	id := uuid.New()
	publicAlias := litellm.PublicKeyAlias(keyTestModel, id)
	internalAlias := litellm.InternalKeyAlias(keyTestModel, id, "slug")

	t.Run("generates the key under the alias the caller chose, pinned to the deployment", func(t *testing.T) {
		lite := &keyLiteLLM{}
		mgr := newKeyManager(lite)

		for _, alias := range []string{publicAlias, internalAlias} {
			key, err := mgr.Generate(context.Background(), Request{Model: keyTestModel, ID: id, Alias: alias})
			require.NoError(t, err)

			assert.Equal(t, lite.keys[alias], key)
			assert.Equal(t, alias, lite.genReq.KeyAlias)
			assert.Equal(t, []string{keyTestModel}, lite.genReq.Models)
			assert.Equal(t, catalogclient.LiteLLMKeyMetadata{Tags: []string{id.String()}}, lite.genReq.Metadata,
				"the key is pinned to its own deployment by tag")
			assert.Equal(t, alias, lite.deleted[len(lite.deleted)-1], "the old key is revoked by the same alias")
		}

		assert.Len(t, lite.keys, 2, "the public and the internal key of one model coexist")
	})

	t.Run("revoke comes before generate", func(t *testing.T) {
		lite := &keyLiteLLM{}

		_, err := newKeyManager(lite).Generate(context.Background(), Request{Model: keyTestModel, ID: id, Alias: publicAlias})
		require.NoError(t, err)
		assert.Equal(t, []string{"delete", "generate"}, lite.calls)
	})

	t.Run("regenerating replaces the key, so only one exists", func(t *testing.T) {
		lite := &keyLiteLLM{}
		mgr := newKeyManager(lite)
		req := Request{Model: keyTestModel, ID: id, Alias: internalAlias}

		first, err := mgr.Generate(context.Background(), req)
		require.NoError(t, err)
		second, err := mgr.Generate(context.Background(), req)
		require.NoError(t, err)

		assert.NotEqual(t, first, second)
		assert.Len(t, lite.keys, 1)
		assert.Equal(t, second, lite.keys[internalAlias])
	})

	t.Run("a model without a name is an internal error and calls nothing", func(t *testing.T) {
		lite := &keyLiteLLM{}

		_, err := newKeyManager(lite).Generate(context.Background(), Request{ID: id, Alias: publicAlias})

		require.Error(t, err)
		var valErr *validators.ValidationError
		assert.False(t, errors.As(err, &valErr), "an internal failure must not look like a client error")
		assert.Empty(t, lite.calls)
	})

	t.Run("revoke failure is 502 and generates nothing", func(t *testing.T) {
		lite := &keyLiteLLM{deleteErr: errors.New("boom")}

		_, err := newKeyManager(lite).Generate(context.Background(), Request{Model: keyTestModel, ID: id, Alias: publicAlias})

		requireBadGateway(t, err)
		assert.Equal(t, []string{"delete"}, lite.calls)
	})

	t.Run("generate failure is 502 and tells the caller to retry", func(t *testing.T) {
		lite := &keyLiteLLM{genErr: errors.New("boom")}

		_, err := newKeyManager(lite).Generate(context.Background(), Request{Model: keyTestModel, ID: id, Alias: publicAlias})

		valErr := requireBadGateway(t, err)
		assert.Contains(t, valErr.Message, "retry")
	})
}

func TestRevoke(t *testing.T) {
	aliases := []string{"a-app-x", "b-app-x"}

	t.Run("revokes every alias in one call", func(t *testing.T) {
		lite := &keyLiteLLM{}

		require.NoError(t, newKeyManager(lite).Revoke(context.Background(), aliases))

		assert.Equal(t, aliases, lite.deleted)
		assert.Equal(t, []string{"delete"}, lite.calls)
	})

	t.Run("no aliases calls nothing", func(t *testing.T) {
		lite := &keyLiteLLM{}

		require.NoError(t, newKeyManager(lite).Revoke(context.Background(), nil))

		assert.Empty(t, lite.calls)
	})

	t.Run("a gateway failure is 502", func(t *testing.T) {
		lite := &keyLiteLLM{deleteErr: errors.New("boom")}

		requireBadGateway(t, newKeyManager(lite).Revoke(context.Background(), aliases))
	})
}
