package openapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid http URL",
			input:    "http://example.com/api.yaml",
			expected: true,
		},
		{
			name:     "valid https URL",
			input:    "https://example.com/api.yaml",
			expected: true,
		},
		{
			name:     "file path not URL",
			input:    "/path/to/file.yaml",
			expected: false,
		},
		{
			name:     "relative path not URL",
			input:    "./file.yaml",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "missing scheme",
			input:    "example.com/api.yaml",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isURL(tt.input)
			if result != tt.expected {
				t.Errorf("isURL(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{
			name:    "load minimal spec",
			file:    "testdata/minimal.yaml",
			wantErr: false,
		},
		{
			name:    "load complex spec",
			file:    "testdata/complex.yaml",
			wantErr: false,
		},
		{
			name:    "file not found",
			file:    "testdata/nonexistent.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := loadFromFile(tt.file)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(data) == 0 {
				t.Error("loadFromFile() returned empty data")
			}
		})
	}
}

func TestLoadFromURL(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.yaml":
			w.WriteHeader(http.StatusOK)
			content, _ := os.ReadFile("testdata/minimal.yaml")
			w.Write(content)
		case "/notfound.yaml":
			w.WriteHeader(http.StatusNotFound)
		case "/error.yaml":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid URL",
			url:     server.URL + "/valid.yaml",
			wantErr: false,
		},
		{
			name:    "404 not found",
			url:     server.URL + "/notfound.yaml",
			wantErr: true,
		},
		{
			name:    "500 server error",
			url:     server.URL + "/error.yaml",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			url:     "http://[::1]:99999/invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := loadFromURL(tt.url, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(data) == 0 {
				t.Error("loadFromURL() returned empty data")
			}
		})
	}
}

// shortenFetchRetry makes loadFromURLWithRetry fast for tests and restores the
// defaults afterwards
func shortenFetchRetry(t *testing.T, timeout time.Duration) {
	t.Helper()
	origTimeout, origInitial, origMax := fetchRetryTimeout, fetchInitialBackoff, fetchMaxBackoff
	fetchRetryTimeout, fetchInitialBackoff, fetchMaxBackoff = timeout, 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() {
		fetchRetryTimeout, fetchInitialBackoff, fetchMaxBackoff = origTimeout, origInitial, origMax
	})
}

func TestLoadFromURLWithRetry(t *testing.T) {
	t.Run("retries 5xx until the service is ready", func(t *testing.T) {
		shortenFetchRetry(t, 5*time.Second)

		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			content, _ := os.ReadFile("testdata/minimal.yaml")
			w.WriteHeader(http.StatusOK)
			w.Write(content)
		}))
		defer server.Close()

		data, err := loadFromURLWithRetry(server.URL+"/openapi.json", false)
		if err != nil {
			t.Fatalf("loadFromURLWithRetry() error = %v", err)
		}
		if len(data) == 0 {
			t.Error("loadFromURLWithRetry() returned empty data")
		}
		if got := calls.Load(); got != 3 {
			t.Errorf("loadFromURLWithRetry() made %d requests, want 3", got)
		}
	})

	t.Run("retries connection refused until the service is listening", func(t *testing.T) {
		shortenFetchRetry(t, 5*time.Second)

		// Reserve a port, then release it so the first attempts are refused
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to reserve port: %v", err)
		}
		addr := listener.Addr().String()
		listener.Close()

		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			content, _ := os.ReadFile("testdata/minimal.yaml")
			w.WriteHeader(http.StatusOK)
			w.Write(content)
		}))
		go func() {
			time.Sleep(50 * time.Millisecond)
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return
			}
			server.Listener = l
			server.Start()
		}()
		defer server.Close()

		if _, err := loadFromURLWithRetry("http://"+addr+"/openapi.json", false); err != nil {
			t.Fatalf("loadFromURLWithRetry() error = %v", err)
		}
	})

	t.Run("gives up after the retry timeout", func(t *testing.T) {
		shortenFetchRetry(t, 100*time.Millisecond)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		start := time.Now()
		if _, err := loadFromURLWithRetry(server.URL+"/openapi.json", false); err == nil {
			t.Fatal("loadFromURLWithRetry() should fail when the service never becomes ready")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("loadFromURLWithRetry() took %s, want it to stop near the 100ms timeout", elapsed)
		}
	})

	t.Run("does not retry 4xx", func(t *testing.T) {
		shortenFetchRetry(t, 5*time.Second)

		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		if _, err := loadFromURLWithRetry(server.URL+"/openapi.json", false); err == nil {
			t.Fatal("loadFromURLWithRetry() should fail on 404")
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("loadFromURLWithRetry() made %d requests, want 1", got)
		}
	})

	t.Run("does not retry TLS verification failures", func(t *testing.T) {
		shortenFetchRetry(t, 5*time.Second)

		var calls atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
		}))
		defer server.Close()

		start := time.Now()
		if _, err := loadFromURLWithRetry(server.URL+"/openapi.json", false); err == nil {
			t.Fatal("loadFromURLWithRetry() should fail against a self-signed certificate")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("loadFromURLWithRetry() took %s, want an immediate failure", elapsed)
		}
	})
}

func TestLoadDescription(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "load minimal spec from file",
			ref:     "testdata/minimal.yaml",
			wantErr: false,
		},
		{
			name:    "load complex spec from file",
			ref:     "testdata/complex.yaml",
			wantErr: false,
		},
		{
			name:    "load spec with refs",
			ref:     "testdata/with-refs.yaml",
			wantErr: false,
		},
		{
			name:    "invalid spec missing info",
			ref:     "testdata/invalid.yaml",
			wantErr: true,
			errMsg:  "missing info section",
		},
		{
			name:    "file not found",
			ref:     "testdata/nonexistent.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadDescription(tt.ref, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadDescription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadDescription() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
			if !tt.wantErr && doc == nil {
				t.Error("LoadDescription() returned nil document")
			}
			if !tt.wantErr && doc.Info == nil {
				t.Error("LoadDescription() returned document with nil Info")
			}
		})
	}
}

func TestLoadDescriptionFromURL(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.yaml" {
			w.WriteHeader(http.StatusOK)
			content, _ := os.ReadFile("testdata/minimal.yaml")
			w.Write(content)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	doc, err := LoadDescription(server.URL + "/api.yaml", false)
	if err != nil {
		t.Fatalf("LoadDescription() from URL failed: %v", err)
	}
	if doc == nil {
		t.Fatal("LoadDescription() from URL returned nil document")
	}
	if doc.Info == nil || doc.Info.Title != "Test API" {
		t.Error("LoadDescription() from URL didn't load correct document")
	}
}

func TestLoadDescriptionRefResolution(t *testing.T) {
	// Load spec with references
	doc, err := LoadDescription("testdata/with-refs.yaml", false)
	if err != nil {
		t.Fatalf("LoadDescription() failed: %v", err)
	}

	// Check that the document loaded
	if doc == nil || doc.Paths == nil {
		t.Fatal("LoadDescription() didn't load document properly")
	}

	// Verify paths exist
	found := false
	for pair := doc.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		if pair.Key() == "/items" {
			found = true
			break
		}
	}
	if !found {
		t.Error("LoadDescription() didn't preserve paths after ref resolution")
	}
}

func TestLoadDescriptionInvalidYAML(t *testing.T) {
	// Create a temporary file with invalid YAML
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(invalidFile, []byte("invalid: yaml: content: ["), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = LoadDescription(invalidFile, false)
	if err == nil {
		t.Error("LoadDescription() should fail on invalid YAML")
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid http URL",
			url:     "http://example.com/api.yaml",
			wantErr: false,
		},
		{
			name:    "valid https URL",
			url:     "https://example.com/api.yaml",
			wantErr: false,
		},
		{
			name:    "valid http URL with port",
			url:     "http://example.com:8080/api.yaml",
			wantErr: false,
		},
		{
			name:    "valid https URL with query params",
			url:     "https://example.com/api.yaml?version=1.0",
			wantErr: false,
		},
		{
			name:    "invalid - file scheme",
			url:     "file:///etc/passwd",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - ftp scheme",
			url:     "ftp://example.com/file.yaml",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - javascript scheme (XSS attempt)",
			url:     "javascript:alert('xss')",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - data scheme",
			url:     "data:text/plain,hello",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - gopher scheme",
			url:     "gopher://example.com",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - no scheme",
			url:     "example.com/api.yaml",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "invalid - malformed URL",
			url:     "ht!tp://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateURL(%q) error = %v, want error containing %q", tt.url, err, tt.errMsg)
				}
			}
		})
	}
}

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid relative path",
			path:    "testdata/minimal.yaml",
			wantErr: false,
		},
		{
			name:    "valid simple filename",
			path:    "config.yaml",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			path:    "configs/prod/api.yaml",
			wantErr: false,
		},
		{
			name:    "invalid - directory traversal with ..",
			path:    "../../../etc/passwd",
			wantErr: true,
			errMsg:  "directory traversal",
		},
		{
			name:    "invalid - directory traversal in middle",
			path:    "configs/../../../etc/passwd",
			wantErr: true,
			errMsg:  "directory traversal",
		},
		{
			name:    "invalid - directory traversal at end",
			path:    "configs/..",
			wantErr: true,
			errMsg:  "directory traversal",
		},
		{
			name:    "invalid - suspicious path with extra slashes",
			path:    "configs//api.yaml",
			wantErr: true,
			errMsg:  "suspicious elements",
		},
		{
			name:    "invalid - path with trailing slash",
			path:    "configs/",
			wantErr: true,
			errMsg:  "suspicious elements",
		},
		{
			name:    "valid - path with dots in filename",
			path:    "api.v1.0.yaml",
			wantErr: false,
		},
		{
			name:    "valid - path with hyphens and underscores",
			path:    "my-api_config.yaml",
			wantErr: false,
		},
		{
			name:    "valid - path with starting /",
			path:    "/Users/test/my-api_config.yaml",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFilePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateFilePath(%q) error = %v, want error containing %q", tt.path, err, tt.errMsg)
				}
			}
		})
	}
}

func TestLoadFromURLWithValidation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "invalid scheme rejected before HTTP request",
			url:     "file:///etc/passwd",
			wantErr: true,
			errMsg:  "invalid URL",
		},
		{
			name:    "ftp scheme rejected",
			url:     "ftp://example.com/file.yaml",
			wantErr: true,
			errMsg:  "invalid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFromURL(tt.url, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadFromURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("loadFromURL(%q) error = %v, want error containing %q", tt.url, err, tt.errMsg)
				}
			}
		})
	}
}

func TestLoadFromFileWithValidation(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "directory traversal rejected",
			path:    "../../../etc/passwd",
			wantErr: true,
			errMsg:  "invalid file path",
		},
		{
			name:    "path with .. rejected",
			path:    "configs/../../../etc/passwd",
			wantErr: true,
			errMsg:  "invalid file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFromFile(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadFromFile(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("loadFromFile(%q) error = %v, want error containing %q", tt.path, err, tt.errMsg)
				}
			}
		})
	}
}

func TestLoadDescriptionWithSecurityValidation(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "file path with directory traversal rejected",
			ref:     "../../../etc/passwd",
			wantErr: true,
			errMsg:  "invalid file path",
		},
		{
			name:    "file:// URL treated as file path and rejected",
			ref:     "file:///etc/passwd",
			wantErr: true,
			errMsg:  "invalid file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadDescription(tt.ref, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadDescription(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadDescription(%q) error = %v, want error containing %q", tt.ref, err, tt.errMsg)
				}
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && s[0:len(substr)] == substr || len(s) > len(substr) && s[len(s)-len(substr):] == substr || (len(substr) > 0 && len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadDescriptionTLSSkipVerify(t *testing.T) {
	// TLS test server presents a self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := os.ReadFile("testdata/minimal.yaml")
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	// Without skip, the self-signed certificate must be rejected
	if _, err := LoadDescription(server.URL+"/api.yaml", false); err == nil {
		t.Error("LoadDescription() should fail against a self-signed certificate when tlsSkipVerify is false")
	}

	// With skip, the description loads
	doc, err := LoadDescription(server.URL+"/api.yaml", true)
	if err != nil {
		t.Fatalf("LoadDescription() with tlsSkipVerify failed: %v", err)
	}
	if doc == nil || doc.Info == nil {
		t.Fatal("LoadDescription() with tlsSkipVerify returned incomplete document")
	}
}
