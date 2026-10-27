package caddy

import (
	"strings"
	"testing"
)

func TestGetCaddyFileContent(t *testing.T) {
	content, err := GetCaddyFileContent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify :443 public ingress rules
	if !strings.Contains(content, ":443") {
		t.Errorf("expected :443 block in Caddyfile")
	}
	if !strings.Contains(content, "respond @tokenize \"Forbidden\" 403") {
		t.Errorf("expected tokenize blocking rule in :443 block")
	}

	// Verify :8443 private ingress rules with mTLS
	if !strings.Contains(content, ":8443") {
		t.Errorf("expected :8443 private ingress block in Caddyfile")
	}
	if !strings.Contains(content, "mode require_and_verify") {
		t.Errorf("expected mTLS client_auth require_and_verify in :8443 block")
	}

	// Verify :8080 private egress / health rules
	if !strings.Contains(content, ":8080") {
		t.Errorf("expected :8080 block in Caddyfile")
	}
	if !strings.Contains(content, "respond /health \"OK\" 200") {
		t.Errorf("expected /health endpoint in :8080 block")
	}
}
