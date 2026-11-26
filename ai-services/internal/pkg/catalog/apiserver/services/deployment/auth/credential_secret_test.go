package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/credentialspec"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

type fakeProvisioner struct {
	credential string
	err        error
	callers    []string
	grants     []map[string][]string
	revoked    []string
}

func (f *fakeProvisioner) ProvisionWithGrants(_ context.Context, caller, deploymentID string, grants map[string][]string) (string, error) {
	f.callers = append(f.callers, caller+"/"+deploymentID)
	f.grants = append(f.grants, grants)
	if f.err != nil {
		return "", f.err
	}

	return f.credential, nil
}

func (f *fakeProvisioner) RevokeForDeployment(_ context.Context, deploymentID string) error {
	f.revoked = append(f.revoked, deploymentID)

	return nil
}

type fakeStore struct {
	secrets   map[string]map[string][]byte
	labels    map[string]map[string]string
	createErr error
	deleted   []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{secrets: map[string]map[string][]byte{}, labels: map[string]map[string]string{}}
}

func (f *fakeStore) CreateSecret(_ context.Context, name string, data map[string][]byte, labels map[string]string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.secrets[name] = data
	f.labels[name] = labels

	return nil
}

func (f *fakeStore) DeleteSecret(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	delete(f.secrets, name)

	return nil
}

var chatSpec = []credentialspec.Spec{{
	ValuesKey: "authServiceCredentialSecret",
	Grants:    map[string][]string{"similarity": {"read"}},
}}

func chatValues() map[string]any {
	return map[string]any{"authServiceCredentialSecret": ""}
}

func TestProvisionCredentialSecrets_StoresCredentialAndSetsOnlyTheName(t *testing.T) {
	p := &fakeProvisioner{credential: "id.topsecret"}
	store := newFakeStore()
	values := chatValues()

	got, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", values, p, store)

	require.NoError(t, err)
	require.Len(t, got.Names(), 1)
	name := got.Names()[0]

	assert.Equal(t, name, values["authServiceCredentialSecret"])
	assert.Equal(t, []byte("id.topsecret"), store.secrets[name]["credential"])
	assert.Equal(t, map[string]string{constants.ApplicationTemplateKey: "dep-1"}, store.labels[name])
	// The credential itself must never be in the values.
	for k, v := range values {
		assert.NotContains(t, v, "topsecret", k)
	}
	// The caller is the service being deployed, with the annotated grants.
	assert.Equal(t, []string{"chat/dep-1"}, p.callers)
	assert.Equal(t, []map[string][]string{{"similarity": {"read"}}}, p.grants)
}

func TestProvisionCredentialSecrets_OverwritesAValueFromTheRequest(t *testing.T) {
	values := map[string]any{"authServiceCredentialSecret": "someone-elses-secret"}

	got, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", values, &fakeProvisioner{credential: "a.b"}, newFakeStore())

	require.NoError(t, err)
	assert.Equal(t, got.Names()[0], values["authServiceCredentialSecret"])
	assert.NotEqual(t, "someone-elses-secret", values["authServiceCredentialSecret"])
}

func TestProvisionCredentialSecrets_NameIsPerDeployment(t *testing.T) {
	a, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", chatValues(), &fakeProvisioner{credential: "a.b"}, newFakeStore())
	require.NoError(t, err)
	b, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-2", chatValues(), &fakeProvisioner{credential: "a.b"}, newFakeStore())
	require.NoError(t, err)

	assert.NotEqual(t, a.Names()[0], b.Names()[0])
	assert.Regexp(t, `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, a.Names()[0], "must be a valid Kubernetes and Podman secret name")
}

func TestProvisionCredentialSecrets_NoSpecsOrNoBackendIsANoOp(t *testing.T) {
	values := chatValues()

	got, err := ProvisionCredentialSecrets(context.Background(), nil, "digitize", "dep", values, &fakeProvisioner{}, newFakeStore())
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep", values, nil, newFakeStore())
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep", values, &fakeProvisioner{}, nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	assert.Equal(t, "", values["authServiceCredentialSecret"])
}

func TestProvisionCredentialSecrets_UndeclaredKeyFailsAndLeavesNothing(t *testing.T) {
	p := &fakeProvisioner{credential: "a.b"}
	store := newFakeStore()

	_, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", map[string]any{}, p, store)

	require.Error(t, err)
	assert.Empty(t, p.callers, "no credential is created for a key the service does not declare")
	assert.Empty(t, store.secrets)
	assert.Equal(t, []string{"dep-1"}, p.revoked, "rolled back")
}

func TestProvisionCredentialSecrets_ProvisionFailureLeavesNothing(t *testing.T) {
	store := newFakeStore()
	_, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", chatValues(), &fakeProvisioner{err: errors.New("db down")}, store)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat")
	assert.Empty(t, store.secrets)
}

func TestProvisionCredentialSecrets_StoreFailureRevokesAndNeverLeaksTheCredential(t *testing.T) {
	p := &fakeProvisioner{credential: "id.topsecret"}
	store := newFakeStore()
	store.createErr = errors.New("podman said: id.topsecret")

	_, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", chatValues(), p, store)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret", "the store's error must not be passed on")
	assert.Equal(t, []string{"dep-1"}, p.revoked)
}

func TestCredentialSecrets_Rollback(t *testing.T) {
	p := &fakeProvisioner{credential: "a.b"}
	store := newFakeStore()
	got, err := ProvisionCredentialSecrets(context.Background(), chatSpec, "chat", "dep-1", chatValues(), p, store)
	require.NoError(t, err)
	name := got.Names()[0]

	got.Rollback(context.Background())

	assert.Equal(t, []string{"dep-1"}, p.revoked)
	assert.Equal(t, []string{name}, store.deleted)
	assert.NotContains(t, store.secrets, name)

	// A nil result (nothing was provisioned) is safe to roll back.
	var none *CredentialSecrets
	assert.NotPanics(t, func() { none.Rollback(context.Background()) })
	assert.Nil(t, none.Names())
}
