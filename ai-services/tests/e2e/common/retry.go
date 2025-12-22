package common

import (
	"log"
	"time"
)

// Retry runs a function multiple times with delay
func Retry(attempts int, delay time.Duration, fn func() error) error {
	for i := 0; i < attempts; i++ {
		if err := fn(); err != nil {
			log.Println("Retry attempt", i+1, "failed:", err)
			time.Sleep(delay)
			continue
		}
		return nil
	}
	return nil
}
