package registry

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// tokenTTLHours is the validity window for single-use bootstrap tokens.
const tokenTTLHours = 24

// TokenRecord holds a single-use bootstrap token bound to a specific worker name.
type TokenRecord struct {
	Token      string
	WorkerName string
	ExpiresAt  time.Time
	Used       bool
}

// TokenStore is an in-memory single-use bootstrap token store.
type TokenStore struct {
	mu     sync.Mutex
	tokens map[string]*TokenRecord
}

// NewTokenStore creates an empty token store.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*TokenRecord)}
}

// IssueToken generates a new 24-hour single-use token bound to workerName and returns it.
// The worker must present this exact token when calling Register; the name supplied in
// the RegisterRequest is ignored — the name bound to the token is authoritative.
func (ts *TokenStore) IssueToken(workerName string) string {
	token := uuid.NewString()
	ts.mu.Lock()
	ts.tokens[token] = &TokenRecord{
		Token:      token,
		WorkerName: workerName,
		ExpiresAt:  time.Now().Add(tokenTTLHours * time.Hour),
	}
	ts.mu.Unlock()

	return token
}

// Validate checks token validity, marks it used, and returns the worker name it was
// issued for. Returns an error if the token is unknown, already used, or expired.
func (ts *TokenStore) Validate(token string) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	rec, ok := ts.tokens[token]
	if !ok {
		return "", fmt.Errorf("bootstrap token not found")
	}
	if rec.Used {
		return "", fmt.Errorf("bootstrap token already used")
	}
	if time.Now().After(rec.ExpiresAt) {
		return "", fmt.Errorf("bootstrap token expired")
	}
	rec.Used = true

	return rec.WorkerName, nil
}
