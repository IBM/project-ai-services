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
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const keyTestModel = "ibm-granite/granite-3.3-8b-instruct"

// keyComponentRepo serves a fixed set of components.
type keyComponentRepo struct {
	dbrepo.ComponentRepository
	components []dbmodels.Component
}

func (r *keyComponentRepo) GetByID(_ context.Context, id uuid.UUID) (*dbmodels.Component, error) {
	for i := range r.components {
		if r.components[i].ID == id {
			return &r.components[i], nil
		}
	}

	return nil, nil
}

// keyConnectorRepo serves a fixed set of connectors.
type keyConnectorRepo struct {
	dbrepo.ConnectorRepository
	connectors []dbmodels.Connector
}

func (r *keyConnectorRepo) GetByID(_ context.Context, id uuid.UUID, _ bool) (*dbmodels.Connector, error) {
	for i := range r.connectors {
		if r.connectors[i].ID == id {
			return &r.connectors[i], nil
		}
	}

	return nil, dbrepo.ErrConnectorNotFound
}

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

func modelMetadata(model string) map[string]any {
	return map[string]any{catalogconstants.MetadataKeyModel: model}
}

func newKeyService(comps []dbmodels.Component, conns []dbmodels.Connector, lite *keyLiteLLM) *Manager {
	if lite.keys == nil {
		lite.keys = map[string]string{}
	}

	return NewManager(&keyComponentRepo{components: comps}, &keyConnectorRepo{connectors: conns}, lite)
}

func TestGenerate(t *testing.T) {
	local := dbmodels.Component{ID: uuid.New(), CreatedBy: "u", Metadata: modelMetadata(keyTestModel)}
	sibling := dbmodels.Component{ID: uuid.New(), CreatedBy: "u", Metadata: modelMetadata(keyTestModel)}
	remote := dbmodels.Connector{
		ID: uuid.New(), CreatedBy: "u", Type: catalogconstants.ConnectorTypeModels,
		Metadata: modelMetadata(keyTestModel),
	}

	t.Run("key is aliased by model name and id", func(t *testing.T) {
		lite := &keyLiteLLM{}
		svc := newKeyService([]dbmodels.Component{local, sibling}, []dbmodels.Connector{remote}, lite)

		for _, id := range []uuid.UUID{local.ID, remote.ID} {
			resp, err := svc.Generate(context.Background(), id, "u")
			require.NoError(t, err)

			alias := keyTestModel + "-" + id.String()
			assert.Equal(t, "public", resp.Type)
			assert.Equal(t, keyTestModel, resp.Model)
			assert.Equal(t, lite.keys[alias], resp.Key)

			assert.Equal(t, alias, lite.genReq.KeyAlias)
			assert.Equal(t, []string{keyTestModel}, lite.genReq.Models)
			assert.Equal(t, catalogclient.LiteLLMKeyMetadata{Tags: []string{id.String()}}, lite.genReq.Metadata,
				"the key is pinned to its own deployment by tag")
			assert.Equal(t, alias, lite.deleted[len(lite.deleted)-1], "the old key is revoked by the same alias")
		}

		assert.Len(t, lite.keys, 2, "each id has its own key")
	})

	t.Run("revoke comes before generate", func(t *testing.T) {
		lite := &keyLiteLLM{}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		_, err := svc.Generate(context.Background(), local.ID, "u")
		require.NoError(t, err)
		assert.Equal(t, []string{"delete", "generate"}, lite.calls)
	})

	t.Run("regenerating replaces the key, so only one exists", func(t *testing.T) {
		lite := &keyLiteLLM{}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		first, err := svc.Generate(context.Background(), local.ID, "u")
		require.NoError(t, err)
		second, err := svc.Generate(context.Background(), local.ID, "u")
		require.NoError(t, err)

		assert.NotEqual(t, first.Key, second.Key)
		assert.Len(t, lite.keys, 1)
		assert.Equal(t, second.Key, lite.keys[keyTestModel+"-"+local.ID.String()])
	})

	t.Run("concurrent requests still leave one key", func(t *testing.T) {
		lite := &keyLiteLLM{}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.Generate(context.Background(), local.ID, "u")
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

	t.Run("unknown id is 404", func(t *testing.T) {
		lite := &keyLiteLLM{}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		_, err := svc.Generate(context.Background(), uuid.New(), "u")
		var valErr *validators.ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, http.StatusNotFound, valErr.Code)
		assert.Empty(t, lite.calls, "nothing is revoked for an unknown model")
	})

	t.Run("a user who did not create the model gets 403 and nothing is revoked", func(t *testing.T) {
		alias := keyTestModel + "-" + local.ID.String()
		lite := &keyLiteLLM{keys: map[string]string{alias: "sk-old"}}
		svc := newKeyService([]dbmodels.Component{local}, []dbmodels.Connector{remote}, lite)

		for _, id := range []uuid.UUID{local.ID, remote.ID} {
			_, err := svc.Generate(context.Background(), id, "someone-else")
			var valErr *validators.ValidationError
			require.ErrorAs(t, err, &valErr)
			assert.Equal(t, http.StatusForbidden, valErr.Code)
		}
		assert.Empty(t, lite.calls)
		assert.Equal(t, "sk-old", lite.keys[alias], "the existing key survives")
	})

	t.Run("revoke failure is 502 and generates nothing", func(t *testing.T) {
		lite := &keyLiteLLM{deleteErr: errors.New("boom")}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		_, err := svc.Generate(context.Background(), local.ID, "u")
		var valErr *validators.ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, http.StatusBadGateway, valErr.Code)
		assert.Equal(t, []string{"delete"}, lite.calls)
	})

	t.Run("generate failure is 502 and tells the caller to retry", func(t *testing.T) {
		lite := &keyLiteLLM{genErr: errors.New("boom")}
		svc := newKeyService([]dbmodels.Component{local}, nil, lite)

		_, err := svc.Generate(context.Background(), local.ID, "u")
		var valErr *validators.ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, http.StatusBadGateway, valErr.Code)
		assert.Contains(t, valErr.Message, "call this endpoint again")
	})
}
