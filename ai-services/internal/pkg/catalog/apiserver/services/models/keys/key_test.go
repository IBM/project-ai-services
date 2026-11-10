package keys

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
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

func newKeyManager(lite *keyLiteLLM) *Manager {
	if lite.keys == nil {
		lite.keys = map[string]string{}
	}

	return NewManager(lite)
}

func modelRow(deploymentType catalogconstants.DeploymentType) *dbrepo.ModelRow {
	return &dbrepo.ModelRow{ID: uuid.New(), DeploymentType: deploymentType, Model: keyTestModel, CreatedBy: "u"}
}

func requireBadGateway(t *testing.T, err error) *validators.ValidationError {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusBadGateway, valErr.Code)

	return valErr
}

func TestGenerate(t *testing.T) {
	local := modelRow(catalogconstants.DeploymentTypeLocal)
	remote := modelRow(catalogconstants.DeploymentTypeRemote)

	t.Run("key is aliased by model name and id", func(t *testing.T) {
		lite := &keyLiteLLM{}
		mgr := newKeyManager(lite)

		for _, row := range []*dbrepo.ModelRow{local, remote} {
			resp, err := mgr.Generate(context.Background(), row)
			require.NoError(t, err)

			alias := keyTestModel + "-" + row.ID.String()
			assert.Equal(t, "public", resp.Type)
			assert.Equal(t, keyTestModel, resp.Model)
			assert.Equal(t, lite.keys[alias], resp.Key)

			assert.Equal(t, alias, lite.genReq.KeyAlias)
			assert.Equal(t, []string{keyTestModel}, lite.genReq.Models)
			assert.Equal(t, catalogclient.LiteLLMKeyMetadata{Tags: []string{row.ID.String()}}, lite.genReq.Metadata,
				"the key is pinned to its own deployment by tag")
			assert.Equal(t, alias, lite.deleted[len(lite.deleted)-1], "the old key is revoked by the same alias")
		}

		assert.Len(t, lite.keys, 2, "each id has its own key")
	})

	t.Run("revoke comes before generate", func(t *testing.T) {
		lite := &keyLiteLLM{}

		_, err := newKeyManager(lite).Generate(context.Background(), local)
		require.NoError(t, err)
		assert.Equal(t, []string{"delete", "generate"}, lite.calls)
	})

	t.Run("regenerating replaces the key, so only one exists", func(t *testing.T) {
		lite := &keyLiteLLM{}
		mgr := newKeyManager(lite)

		first, err := mgr.Generate(context.Background(), local)
		require.NoError(t, err)
		second, err := mgr.Generate(context.Background(), local)
		require.NoError(t, err)

		assert.NotEqual(t, first.Key, second.Key)
		assert.Len(t, lite.keys, 1)
		assert.Equal(t, second.Key, lite.keys[keyTestModel+"-"+local.ID.String()])
	})

	t.Run("concurrent requests still leave one key", func(t *testing.T) {
		lite := &keyLiteLLM{}
		mgr := newKeyManager(lite)

		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := mgr.Generate(context.Background(), local)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err, "an interleaved revoke and generate would hit an existing alias")
		}
		assert.Len(t, lite.keys, 1)
	})

	t.Run("a model without a name is an internal error and calls nothing", func(t *testing.T) {
		lite := &keyLiteLLM{}
		row := modelRow(catalogconstants.DeploymentTypeLocal)
		row.Model = ""

		_, err := newKeyManager(lite).Generate(context.Background(), row)

		require.Error(t, err)
		var valErr *validators.ValidationError
		assert.False(t, errors.As(err, &valErr), "an internal failure must not look like a client error")
		assert.Empty(t, lite.calls)
	})

	t.Run("revoke failure is 502 and generates nothing", func(t *testing.T) {
		lite := &keyLiteLLM{deleteErr: errors.New("boom")}

		_, err := newKeyManager(lite).Generate(context.Background(), local)

		requireBadGateway(t, err)
		assert.Equal(t, []string{"delete"}, lite.calls)
	})

	t.Run("generate failure is 502 and tells the caller to retry", func(t *testing.T) {
		lite := &keyLiteLLM{genErr: errors.New("boom")}

		_, err := newKeyManager(lite).Generate(context.Background(), local)

		valErr := requireBadGateway(t, err)
		assert.Contains(t, valErr.Message, "call this endpoint again")
	})
}
