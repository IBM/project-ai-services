package utils

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ctx        = context.Background()
	ErrTimeout = errors.New("operation timed out")
)

// RunWithTimeout executes `fn` with a timeout.
func RunWithTimeout(timeout time.Duration, fn func(ctx context.Context) error) error {
	if timeout <= 0 {
		// If no timeout → run normally
		return fn(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		errCh <- fn(ctx)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("%w after %s", ErrTimeout, timeout)
	case err := <-errCh:
		return err
	}
}
