package common

import (
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Retry runs a function multiple times with delay.
func Retry(attempts int, delay time.Duration, fn func() error) error {
	var lastErr error

	for i := range attempts {
		if err := fn(); err != nil {
			lastErr = err
			logger.Warningf(
				"Retry attempt %d/%d failed: %v",
				i+1,
				attempts,
				err,
			)
			time.Sleep(delay)

			continue
		}

		return nil
	}

	return lastErr
}
