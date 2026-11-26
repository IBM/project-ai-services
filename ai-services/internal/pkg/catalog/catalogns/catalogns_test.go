package catalogns

import (
	"strings"
	"testing"
)

func TestGetSet(t *testing.T) {
	t.Cleanup(func() { _ = Set(Default) })

	if got := Get(); got != Default {
		t.Fatalf("Get() = %q, want default %q", got, Default)
	}
	if err := Set("ai-services-dev"); err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
	if got := Get(); got != "ai-services-dev" {
		t.Errorf("Get() = %q, want ai-services-dev", got)
	}
}

func TestSetValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{"valid", "ai-services-dev", true},
		{"max length", strings.Repeat("a", MaxLen), true},
		{"too long for app suffix", strings.Repeat("a", MaxLen+1), false},
		{"uppercase", "AI-Services", false},
		{"underscore", "ai_services", false},
		{"leading dash", "-ai", false},
		{"empty", "", false},
		{"slash", "a/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { _ = Set(Default) })
			err := Set(tt.value)
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if got := Get(); got != Default {
					t.Errorf("namespace changed to %q on invalid input", got)
				}
			}
		})
	}
}
