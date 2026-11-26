package common

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeRevoker struct {
	revoked []string
	err     error
}

func (f *fakeRevoker) RevokeForDeployment(_ context.Context, deploymentID string) error {
	f.revoked = append(f.revoked, deploymentID)

	return f.err
}

func TestRevokeServiceCredentials_RevokesByServiceID(t *testing.T) {
	r := &fakeRevoker{}
	id := uuid.New()

	RevokeServiceCredentials(context.Background(), r, id)

	assert.Equal(t, []string{id.String()}, r.revoked)
}

func TestRevokeServiceCredentials_NilRevokerIsANoOp(t *testing.T) {
	assert.NotPanics(t, func() {
		RevokeServiceCredentials(context.Background(), nil, uuid.New())
	})
}

func TestRevokeServiceCredentials_FailureIsNotPropagated(t *testing.T) {
	r := &fakeRevoker{err: errors.New("db down")}

	assert.NotPanics(t, func() {
		RevokeServiceCredentials(context.Background(), r, uuid.New())
	})
	assert.Len(t, r.revoked, 1)
}
