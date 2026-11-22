package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvisioner struct {
	calls      []string
	credential string
	err        error
}

func (f *fakeProvisioner) ProvisionIfNeeded(_ context.Context, serviceID, deploymentID string) (string, error) {
	f.calls = append(f.calls, serviceID+"/"+deploymentID)
	if f.err != nil {
		return "", f.err
	}
	if serviceID != "chat" {
		return "", nil
	}

	return f.credential, nil
}

func TestInjectServiceCredential_Chat(t *testing.T) {
	values := map[string]any{}
	p := &fakeProvisioner{credential: "id.secret"}

	require.NoError(t, InjectServiceCredential(context.Background(), "chat", "dep-1", values, p))

	assert.Equal(t, "id.secret", values["authServiceCredential"])
	assert.Equal(t, []string{"chat/dep-1"}, p.calls)
}

func TestInjectServiceCredential_ServiceThatNeedsNone(t *testing.T) {
	for _, svc := range []string{"similarity", "summarize", "digitize", "other"} {
		values := map[string]any{}
		require.NoError(t, InjectServiceCredential(context.Background(), svc, "dep", values, &fakeProvisioner{credential: "x.y"}))
		assert.NotContains(t, values, "authServiceCredential", svc)
	}
}

func TestInjectServiceCredential_NilProvisionerOrValuesIsANoOp(t *testing.T) {
	values := map[string]any{}
	require.NoError(t, InjectServiceCredential(context.Background(), "chat", "dep", values, nil))
	assert.Empty(t, values)

	require.NoError(t, InjectServiceCredential(context.Background(), "chat", "dep", nil, &fakeProvisioner{credential: "x.y"}))
}

func TestInjectServiceCredential_ErrorPropagatesWithoutAValue(t *testing.T) {
	values := map[string]any{}
	err := InjectServiceCredential(context.Background(), "chat", "dep", values, &fakeProvisioner{err: errors.New("db down")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat")
	assert.NotContains(t, values, "authServiceCredential")
}
