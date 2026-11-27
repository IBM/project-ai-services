package utils

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// setDeletionPolling overrides the polling settings for the duration of a test.
func setDeletionPolling(t *testing.T, interval time.Duration, attempts int) {
	t.Helper()

	origInterval, origAttempts := deletionPollInterval, deletionMaxAttempts
	deletionPollInterval, deletionMaxAttempts = interval, attempts
	t.Cleanup(func() { deletionPollInterval, deletionMaxAttempts = origInterval, origAttempts })
}

func TestWaitForDeletion_NotFoundAfterPolling(t *testing.T) {
	setDeletionPolling(t, time.Millisecond, 5)

	calls := 0
	fetch := func(context.Context) error {
		calls++
		if calls < 3 {
			return nil
		}

		return &catalogClient.HTTPError{StatusCode: http.StatusNotFound}
	}

	err := WaitForDeletion(context.Background(), "Model", fetch)

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestWaitForDeletion_Timeout(t *testing.T) {
	setDeletionPolling(t, time.Millisecond, 2)

	err := WaitForDeletion(context.Background(), "Model", func(context.Context) error { return nil })

	assert.ErrorContains(t, err, "timeout waiting for model deletion")
}

func TestWaitForDeletion_FetchError(t *testing.T) {
	setDeletionPolling(t, time.Millisecond, 2)

	fetch := func(context.Context) error {
		return &catalogClient.HTTPError{StatusCode: http.StatusInternalServerError}
	}

	err := WaitForDeletion(context.Background(), "Application", fetch)

	var httpErr *catalogClient.HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.ErrorContains(t, err, "failed to fetch application")
}

func TestWaitForDeletion_ContextCancelled(t *testing.T) {
	setDeletionPolling(t, time.Hour, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WaitForDeletion(ctx, "Model", func(context.Context) error { return nil })

	assert.ErrorIs(t, err, context.Canceled)
}
