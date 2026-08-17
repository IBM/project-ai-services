package registry

import (
	"testing"
	"time"
)

func TestTokenStore_IssueAndValidate(t *testing.T) {
	ts := NewTokenStore()
	token := ts.IssueToken("worker-a")

	if token == "" {
		t.Fatal("expected non-empty token")
	}

	name, err := ts.Validate(token)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if name != "worker-a" {
		t.Fatalf("expected worker name %q, got %q", "worker-a", name)
	}
}

func TestTokenStore_SingleUse(t *testing.T) {
	ts := NewTokenStore()
	token := ts.IssueToken("worker-b")

	if _, err := ts.Validate(token); err != nil {
		t.Fatalf("first Validate: unexpected error: %v", err)
	}

	if _, err := ts.Validate(token); err == nil {
		t.Fatal("second Validate: expected error for already-used token")
	}
}

func TestTokenStore_UnknownToken(t *testing.T) {
	ts := NewTokenStore()

	if _, err := ts.Validate("not-a-real-token"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}

func TestTokenStore_ExpiredToken(t *testing.T) {
	ts := NewTokenStore()

	// Manually inject an expired record.
	ts.mu.Lock()
	ts.tokens["expired"] = &TokenRecord{
		Token:      "expired",
		WorkerName: "worker-exp",
		ExpiresAt:  time.Now().Add(-time.Hour),
	}
	ts.mu.Unlock()

	if _, err := ts.Validate("expired"); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestTokenStore_MultipleWorkers(t *testing.T) {
	ts := NewTokenStore()

	tokenA := ts.IssueToken("worker-a")
	tokenB := ts.IssueToken("worker-b")

	nameA, err := ts.Validate(tokenA)
	if err != nil {
		t.Fatalf("Validate tokenA: %v", err)
	}
	nameB, err := ts.Validate(tokenB)
	if err != nil {
		t.Fatalf("Validate tokenB: %v", err)
	}

	if nameA != "worker-a" {
		t.Errorf("expected worker-a, got %q", nameA)
	}
	if nameB != "worker-b" {
		t.Errorf("expected worker-b, got %q", nameB)
	}
}
