package utils

import (
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// BackoffFunc type definition
type BackoffFunc func(currentDelay time.Duration) time.Duration

// Retry -> retries based on the retry attempts and initialDelay time set on failure.
// Does exponentialBackOff based on the provided BackoffFunc
// Set backoff func to nil, if exponentialBackoff is not required
func Retry(
	attempts int,
	initialDelay time.Duration,
	backoff BackoffFunc,
	fn func() error,
) error {
	delay := initialDelay
	var err error

	// Run the function initially and if no error do not proceed with retry attempts
	err = fn()
	if err == nil {
		return nil
	}

	for i := range attempts {
		logger.Infof("\n[Retry] Attempt %d/%d...\n", i+1, attempts, 0)

		if err = fn(); err == nil {
			return nil
		}

		// At Last attempt — stop
		if i == attempts-1 {
			break
		}

		// Sleep till delay
		logger.Infof("[Retry] Sleeping %v before retrying...\n", delay, 2)
		time.Sleep(delay)

		// Apply backoff if provided
		if backoff != nil {
			delay = backoff(delay)
		}
	}

	return fmt.Errorf("retry failed after %d attempts with err: %w", attempts, err)
}
