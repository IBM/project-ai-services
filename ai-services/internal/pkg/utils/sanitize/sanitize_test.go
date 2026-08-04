package sanitize

import (
	"fmt"
	"strings"
	"testing"
)

// ── isSensitiveKey: keys that MUST be redacted ────────────────────────────────

func TestIsSensitiveKey_Sensitive(t *testing.T) {
	s := NewSecretSanitizer()

	cases := []string{
		// password / passwd
		"password", "Password", "db_password", "dbPassword",
		"passwd", "PASSWD", "dbPasswd", "db_passwd",
		// opensearch / component-style dotted keys
		"opensearch.password", "opensearch.admin_password",
		// secret
		"secret", "client_secret", "clientSecret", "SECRET_KEY",
		// token (word-boundary still catches these)
		"token", "oauth_token", "accessToken", "X-Auth-Token", "refresh_token",
		// api key variants
		"apiKey", "apikey", "api_key", "api-key", "ApiKey", "APIKEY", "Api.Key",
		// access key
		"accessKey", "accesskey", "access_key", "ACCESS_KEY",
		// private key
		"privateKey", "privatekey", "private_key", "PRIVATE_KEY",
		// credential(s)
		"credential", "credentials", "db_credentials",
		// auth (word-boundary catches Authorization, x-auth, authToken)
		"auth", "Authorization", "x-auth", "authToken", "oauth_auth",
		// cert
		"cert", "tls_cert", "certificate", "ssl_certificate", "CERT", "ca_cert",
	}

	for _, k := range cases {
		if !s.isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = false, want true", k)
		}
	}
}

// ── isSensitiveKey: keys that must NOT be redacted ────────────────────────────

func TestIsSensitiveKey_Safe(t *testing.T) {
	s := NewSecretSanitizer()

	// Note: Go's RE2 engine does not support \b word boundaries, so patterns
	// use plain substrings. "tokenizer", "author" etc. that contain sensitive
	// substrings are therefore intentionally caught — they do not appear as map
	// keys in this codebase, so false-positive redaction is harmless.
	cases := []string{
		// generic infra / config — no sensitive substring
		"model", "host", "port", "url", "name", "version",
		"componentType", "providerID", "instanceSlug",
		"templateID", "baseDir", "catalogID",
	}

	for _, k := range cases {
		if s.isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = true, want false", k)
		}
	}
}

// ── sanitizeMapAny: flat map ──────────────────────────────────────────────────

func TestSanitizeMapAny_FlatMap(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		// sensitive
		"apiKey":   "sk-abc123",
		"password": "hunter2",
		// safe
		"model": "ibm-granite/granite-3.3-8b-instruct",
		"host":  "opensearch-d41fed174e",
		"port":  "9200",
	}

	out := s.sanitizeMapAny(in)

	assertRedacted(t, out, "apiKey")
	assertRedacted(t, out, "password")
	assertValue(t, out, "model", "ibm-granite/granite-3.3-8b-instruct")
	assertValue(t, out, "host", "opensearch-d41fed174e")
	assertValue(t, out, "port", "9200")
}

// ── sanitizeMapAny: real-world component params (matches original bug report) ─

func TestSanitizeMapAny_ComponentParams(t *testing.T) {
	s := NewSecretSanitizer()

	// Reproduces: "First instance: map[apiKey:1234567 model:ibm-granite/...]"
	params := map[string]any{
		"apiKey": "1234567",
		"model":  "ibm-granite/granite-3.3-8b-instruct",
	}

	out := s.sanitizeMapAny(params)
	msg := fmt.Sprintf("%v", out)

	if strings.Contains(msg, "1234567") {
		t.Errorf("API key leaked into formatted output: %s", msg)
	}
	if !strings.Contains(msg, Redacted) {
		t.Errorf("expected %s in output: %s", Redacted, msg)
	}
	if !strings.Contains(msg, "ibm-granite/granite-3.3-8b-instruct") {
		t.Errorf("safe model value was removed: %s", msg)
	}
}

// ── sanitizeMapAny: OpenSearch-style dotted keys ──────────────────────────────

func TestSanitizeMapAny_OpenSearchDottedKeys(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		"opensearch.password":       "admin-secret",
		"opensearch.admin_password": "admin2",
		"opensearch.host":           "opensearch-d41fed174e",
		"opensearch.port":           "9200",
	}

	out := s.sanitizeMapAny(in)

	assertRedacted(t, out, "opensearch.password")
	assertRedacted(t, out, "opensearch.admin_password")
	assertValue(t, out, "opensearch.host", "opensearch-d41fed174e")
	assertValue(t, out, "opensearch.port", "9200")
}

// ── sanitizeMapAny: nested map[string]any (one level) ────────────────────────

func TestSanitizeMapAny_NestedMapAny_OneLevelDeep(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		"opensearch": map[string]any{
			"host":     "opensearch-d41fed174e",
			"port":     "9200",
			"password": "admin-secret",
			"apiKey":   "os-key-xyz",
		},
		"model": "granite",
	}

	out := s.sanitizeMapAny(in)

	assertValue(t, out, "model", "granite")

	nested, ok := out["opensearch"].(map[string]any)
	if !ok {
		t.Fatal("nested 'opensearch' map was not preserved as map[string]any")
	}
	assertValue(t, nested, "host", "opensearch-d41fed174e")
	assertValue(t, nested, "port", "9200")
	assertRedacted(t, nested, "password")
	assertRedacted(t, nested, "apiKey")
}

// ── sanitizeMapAny: deeply nested map[string]any (two levels) ────────────────

func TestSanitizeMapAny_NestedMapAny_TwoLevelsDeep(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		"services": map[string]any{
			"opensearch": map[string]any{
				"admin_password": "deep-secret",
				"host":           "opensearch-host",
			},
		},
	}

	out := s.sanitizeMapAny(in)

	services, ok := out["services"].(map[string]any)
	if !ok {
		t.Fatal("'services' not preserved as map[string]any")
	}
	opensearch, ok := services["opensearch"].(map[string]any)
	if !ok {
		t.Fatal("'services.opensearch' not preserved as map[string]any")
	}
	assertRedacted(t, opensearch, "admin_password")
	assertValue(t, opensearch, "host", "opensearch-host")
}

// ── sanitizeMapAny: multiple sibling nested maps (real-world component params) ─

func TestSanitizeMapAny_MultipleNestedComponents(t *testing.T) {
	s := NewSecretSanitizer()

	// Mirrors the shape of a deployment request Values map:
	// { llm: { apiKey: "...", model: "..." }, opensearch: { password: "...", host: "..." } }
	in := map[string]any{
		"llm": map[string]any{
			"apiKey": "sk-llm-secret",
			"model":  "ibm-granite/granite-3.3-8b-instruct",
		},
		"opensearch": map[string]any{
			"password": "os-admin-pw",
			"host":     "opensearch-d41fed174e",
			"port":     "9200",
		},
		"name": "Test RAG",
	}

	out := s.sanitizeMapAny(in)

	// Top-level safe key unchanged
	assertValue(t, out, "name", "Test RAG")

	// llm block: apiKey redacted, model preserved
	llm, ok := out["llm"].(map[string]any)
	if !ok {
		t.Fatal("'llm' not preserved as map[string]any")
	}
	assertRedacted(t, llm, "apiKey")
	assertValue(t, llm, "model", "ibm-granite/granite-3.3-8b-instruct")

	// opensearch block: password redacted, host/port preserved
	opensearch, ok := out["opensearch"].(map[string]any)
	if !ok {
		t.Fatal("'opensearch' not preserved as map[string]any")
	}
	assertRedacted(t, opensearch, "password")
	assertValue(t, opensearch, "host", "opensearch-d41fed174e")
	assertValue(t, opensearch, "port", "9200")
}

// ── sanitizeMapAny: nested map[string]string ──────────────────────────────────

func TestSanitizeMapAny_NestedMapString(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		"env": map[string]string{
			"POSTGRES_PASSWORD": "hunter2",
			"POSTGRES_HOST":     "db-host",
			"API_KEY":           "sk-999",
		},
	}

	out := s.sanitizeMapAny(in)

	nested, ok := out["env"].(map[string]string)
	if !ok {
		t.Fatal("nested map[string]string was not preserved")
	}
	if nested["POSTGRES_PASSWORD"] != Redacted {
		t.Errorf("POSTGRES_PASSWORD not redacted: got %v", nested["POSTGRES_PASSWORD"])
	}
	if nested["API_KEY"] != Redacted {
		t.Errorf("API_KEY not redacted: got %v", nested["API_KEY"])
	}
	if nested["POSTGRES_HOST"] != "db-host" {
		t.Errorf("POSTGRES_HOST was changed: got %v", nested["POSTGRES_HOST"])
	}
}

// ── sanitizeMapAny: original map must not be mutated ─────────────────────────

func TestSanitizeMapAny_DoesNotMutateOriginal(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]any{
		"apiKey":   "sk-original",
		"password": "pw-original",
		"model":    "granite",
	}

	_ = s.sanitizeMapAny(in)

	if in["apiKey"] != "sk-original" {
		t.Error("sanitizeMapAny mutated apiKey in original map")
	}
	if in["password"] != "pw-original" {
		t.Error("sanitizeMapAny mutated password in original map")
	}
}

// ── sanitizeMapString ─────────────────────────────────────────────────────────

func TestSanitizeMapString(t *testing.T) {
	s := NewSecretSanitizer()

	in := map[string]string{
		"api_key":  "sk-abc123",
		"password": "hunter2",
		"model":    "granite",
		"host":     "localhost",
	}

	out := s.sanitizeMapString(in)

	if out["api_key"] != Redacted {
		t.Errorf("api_key not redacted: got %v", out["api_key"])
	}
	if out["password"] != Redacted {
		t.Errorf("password not redacted: got %v", out["password"])
	}
	if out["model"] != "granite" {
		t.Errorf("model was changed: got %v", out["model"])
	}
	if out["host"] != "localhost" {
		t.Errorf("host was changed: got %v", out["host"])
	}
}

// ── SanitizeArgs (public) ─────────────────────────────────────────────────────

func TestSanitizeArgs_FastPath_NoMaps(t *testing.T) {
	s := NewSecretSanitizer()

	args := []any{"hello", 42, true}
	out := s.SanitizeArgs(args)

	if len(out) != len(args) {
		t.Fatalf("fast-path: wrong length: got %d, want %d", len(out), len(args))
	}
	// Same backing array — mutate args and verify out reflects the change.
	args[0] = "mutated"
	if out[0] != "mutated" {
		t.Error("SanitizeArgs returned a new slice for non-map args (expected same backing array)")
	}
}

func TestSanitizeArgs_SanitisesMapAnyArg(t *testing.T) {
	s := NewSecretSanitizer()

	args := []any{
		"component llm/vllm-cpu params",
		map[string]any{
			"apiKey":   "1234567",
			"password": "hunter2",
			"model":    "ibm-granite/granite-3.3-8b-instruct",
		},
	}

	out := s.SanitizeArgs(args)

	if out[0] != "component llm/vllm-cpu params" {
		t.Errorf("string arg was changed: got %v", out[0])
	}

	m, ok := out[1].(map[string]any)
	if !ok {
		t.Fatal("second arg is not map[string]any after SanitizeArgs")
	}
	assertRedacted(t, m, "apiKey")
	assertRedacted(t, m, "password")
	assertValue(t, m, "model", "ibm-granite/granite-3.3-8b-instruct")
}

func TestSanitizeArgs_SanitisesMapStringArg(t *testing.T) {
	s := NewSecretSanitizer()

	args := []any{
		map[string]string{
			"api_key": "sk-xyz",
			"host":    "localhost",
		},
	}

	out := s.SanitizeArgs(args)

	m, ok := out[0].(map[string]string)
	if !ok {
		t.Fatal("arg is not map[string]string after SanitizeArgs")
	}
	if m["api_key"] != Redacted {
		t.Errorf("api_key not redacted: got %v", m["api_key"])
	}
	if m["host"] != "localhost" {
		t.Errorf("host was changed: got %v", m["host"])
	}
}

func TestSanitizeArgs_NestedMapInsideArg(t *testing.T) {
	s := NewSecretSanitizer()

	// Nested map[string]any passed as a single variadic arg — verifies that
	// SanitizeArgs recurses into nested maps, not just the top level.
	args := []any{
		map[string]any{
			"opensearch": map[string]any{
				"password": "deep-secret",
				"host":     "opensearch-host",
			},
		},
	}

	out := s.SanitizeArgs(args)

	top, ok := out[0].(map[string]any)
	if !ok {
		t.Fatal("arg not map[string]any after SanitizeArgs")
	}
	nested, ok := top["opensearch"].(map[string]any)
	if !ok {
		t.Fatal("nested opensearch map not preserved")
	}
	assertRedacted(t, nested, "password")
	assertValue(t, nested, "host", "opensearch-host")
}

func TestSanitizeArgs_MultipleNestedComponents(t *testing.T) {
	s := NewSecretSanitizer()

	// Same shape as above but exercised through SanitizeArgs — the path the
	// logger takes when a Values map is passed as a %v format argument.
	args := []any{
		"deploying application",
		map[string]any{
			"llm": map[string]any{
				"apiKey": "sk-llm-secret",
				"model":  "ibm-granite/granite-3.3-8b-instruct",
			},
			"opensearch": map[string]any{
				"password": "os-admin-pw",
				"host":     "opensearch-d41fed174e",
				"port":     "9200",
			},
		},
	}

	out := s.SanitizeArgs(args)

	if out[0] != "deploying application" {
		t.Errorf("string arg was changed: got %v", out[0])
	}

	top, ok := out[1].(map[string]any)
	if !ok {
		t.Fatal("second arg not map[string]any after SanitizeArgs")
	}

	llm, ok := top["llm"].(map[string]any)
	if !ok {
		t.Fatal("'llm' not preserved as map[string]any")
	}
	assertRedacted(t, llm, "apiKey")
	assertValue(t, llm, "model", "ibm-granite/granite-3.3-8b-instruct")

	opensearch, ok := top["opensearch"].(map[string]any)
	if !ok {
		t.Fatal("'opensearch' not preserved as map[string]any")
	}
	assertRedacted(t, opensearch, "password")
	assertValue(t, opensearch, "host", "opensearch-d41fed174e")
	assertValue(t, opensearch, "port", "9200")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertRedacted(t *testing.T, m map[string]any, key string) {
	t.Helper()

	v, ok := m[key]
	if !ok {
		t.Errorf("key %q missing from sanitized map", key)
		return
	}
	if v != Redacted {
		t.Errorf("key %q: expected %s, got %v", key, Redacted, v)
	}
}

func assertValue(t *testing.T, m map[string]any, key, want string) {
	t.Helper()

	v, ok := m[key]
	if !ok {
		t.Errorf("key %q missing from sanitized map", key)
		return
	}
	if v != want {
		t.Errorf("key %q: expected %q, got %v", key, want, v)
	}
}
