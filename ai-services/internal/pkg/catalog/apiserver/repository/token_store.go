package repository

import (
	"sync"
	"time"
)

// TokenBlacklist defines the interface for managing revoked tokens. It allows adding tokens to the blacklist
// with their expiry times and checking if a token is currently blacklisted.
type TokenBlacklist interface {
	Add(token string, exp time.Time)
	Contains(token string) bool
	Stop()
}

// InMemoryTokenBlacklist is a simple in-memory implementation of a token blacklist.
// It stores revoked tokens along with their expiry times and provides methods to add tokens,
// check for their presence, and clean up expired tokens.
type InMemoryTokenBlacklist struct {
	mu     sync.RWMutex
	tokens map[string]time.Time // token -> expiry
	stopCh chan struct{}
}

// TODO: Use a more robust for multi-instance deployments, e.g. Redis with TTL or a distributed cache like Memcached.
// This in-memory version is only suitable for single-instance setups or testing.

// NewInMemoryTokenBlacklist creates a new instance of InMemoryTokenBlacklist and starts the garbage collection goroutine.
func NewInMemoryTokenBlacklist() *InMemoryTokenBlacklist {
	b := &InMemoryTokenBlacklist{
		tokens: make(map[string]time.Time),
		stopCh: make(chan struct{}),
	}
	go b.gc()

	return b
}

// Add adds a token to the blacklist with its expiry time. The token will be considered invalid until it expires.
func (b *InMemoryTokenBlacklist) Add(token string, exp time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens[token] = exp
}

// Contains checks if the provided token string is in the blacklist and has not yet expired.
// If the token is found in the blacklist but has expired, it will be removed from the blacklist.
func (b *InMemoryTokenBlacklist) Contains(token string) bool {
	now := time.Now()

	// Fast path: read lock
	b.mu.RLock()
	exp, ok := b.tokens[token]
	if !ok {
		b.mu.RUnlock()

		return false
	}
	if now.Before(exp) {
		b.mu.RUnlock()

		return true // revoked and not yet expired
	}
	b.mu.RUnlock()

	// Slow path: token found but expired -> delete under write lock
	b.mu.Lock()
	// Re-check under write lock in case of races
	if exp2, ok2 := b.tokens[token]; ok2 && now.After(exp2) {
		delete(b.tokens, token)
	}
	b.mu.Unlock()

	return false
}

// Stop signals the garbage collection goroutine to stop. This should be called when the blacklist is no longer needed to clean up resources.
func (b *InMemoryTokenBlacklist) Stop() { close(b.stopCh) }

// gc runs periodically to clean up expired tokens from the blacklist to prevent unbounded memory growth.
func (b *InMemoryTokenBlacklist) gc() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			b.mu.Lock()
			for t, exp := range b.tokens {
				if now.After(exp) {
					delete(b.tokens, t)
				}
			}
			b.mu.Unlock()
		}
	}
}
