package caddy

import (
	"strings"
	"testing"

	"github.com/project-ai-services/ai-services/assets"
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

func TestCaddyPodTemplatePorts(t *testing.T) {
	// Verify that caddy template files exist and render port annotations
	catalogCaddyTmpl, err := assets.CatalogFS.ReadFile("catalog/podman/templates/caddy.yaml.tmpl")
	if err != nil {
		t.Fatalf("failed to read catalog caddy template: %v", err)
	}

	tmplStr := string(catalogCaddyTmpl)
	if !strings.Contains(tmplStr, "8443") {
		t.Errorf("expected 8443 port in catalog caddy template")
	}
	if !strings.Contains(tmplStr, "8080") {
		t.Errorf("expected 8080 port in catalog caddy template")
	}

	workerCaddyTmpl, err := assets.WorkerFS.ReadFile("worker/podman/templates/caddy.yaml.tmpl")
	if err != nil {
		t.Fatalf("failed to read worker caddy template: %v", err)
	}

	workerTmplStr := string(workerCaddyTmpl)
	if !strings.Contains(workerTmplStr, "8443") {
		t.Errorf("expected 8443 port in worker caddy template")
	}
	if !strings.Contains(workerTmplStr, "8080") {
		t.Errorf("expected 8080 port in worker caddy template")
	}
}
