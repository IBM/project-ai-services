package join

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/project-ai-services/ai-services/assets"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const testMTLSSecret = "test-mtls-secret"

// writeTestCredentials writes a matching cert/key/CA set into a temp dir through
// writeTLSMaterial and returns the directory.
func writeTestCredentials(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv(workerconstants.MTLSEncryptionKeyEnv, testMTLSSecret)

	certPEM, keyPEM := newSelfSignedPair(t)
	if err := writeTLSMaterial(dir, certPEM, keyPEM, certPEM); err != nil {
		t.Fatalf("writeTLSMaterial: %v", err)
	}

	return dir
}

func TestWriteTLSMaterial_KeyStaysEncryptedAndCaddyReadable(t *testing.T) {
	dir := writeTestCredentials(t)

	enc, err := os.ReadFile(filepath.Join(dir, tlsKeyFile))
	if err != nil {
		t.Fatalf("read %s: %v", tlsKeyFile, err)
	}
	if strings.Contains(string(enc), "PRIVATE KEY") {
		t.Errorf("%s must stay encrypted at rest", tlsKeyFile)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "PRIVATE KEY") {
			t.Errorf("%s holds a plaintext private key; none may be written to disk", e.Name())
		}
	}

	assertCaddyReadable(t, dir)
}

// TestLoadClientCert_DoesNotChangePermissions guards against loadClientCert (and so
// hasValidTLSCredentials) modifying the filesystem.
func TestLoadClientCert_DoesNotChangePermissions(t *testing.T) {
	dir := writeTestCredentials(t)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, tlsKeyFile), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadClientCert(dir); err != nil {
		t.Fatalf("loadClientCert: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	keyInfo, err := os.Stat(filepath.Join(dir, tlsKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || keyInfo.Mode().Perm() != 0o600 {
		t.Errorf("loadClientCert changed modes: dir %v, key %v", dirInfo.Mode().Perm(), keyInfo.Mode().Perm())
	}
}

func assertCaddyReadable(t *testing.T, dir string) {
	t.Helper()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm()&0o055 != 0o055 {
		t.Errorf("dir mode %v: must be traversable by the Caddy user", dirInfo.Mode().Perm())
	}

	keyInfo, err := os.Stat(filepath.Join(dir, tlsKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm()&0o044 != 0o044 {
		t.Errorf("%s mode %v: the encrypted key must be readable by the Caddy user", tlsKeyFile, keyInfo.Mode().Perm())
	}
}

// TestWorkerCaddyfile_PrivateIngressUsesWorkerTLSFiles keeps the Caddyfile in step
// with the file names the worker writes, and checks that no plaintext key is used.
func TestWorkerCaddyfile_PrivateIngressUsesWorkerTLSFiles(t *testing.T) {
	raw, err := assets.WorkerFS.ReadFile("worker/podman/Caddyfile.tmpl")
	if err != nil {
		t.Fatalf("read worker Caddyfile: %v", err)
	}

	want := []string{
		"get_certificate encrypted_file",
		"cert        " + filepath.Join(workerconstants.WorkerTLSDir, tlsCertFile),
		"key         " + filepath.Join(workerconstants.WorkerTLSDir, tlsKeyFile),
		"secret_file /etc/secret/worker-mtls-encryption-secret/mtls-encryption-key",
		"trusted_ca_cert_file " + filepath.Join(workerconstants.WorkerTLSDir, caCertFile),
	}
	for _, s := range want {
		if !strings.Contains(string(raw), s) {
			t.Errorf("worker Caddyfile.tmpl does not reference %q", s)
		}
	}
	if strings.Contains(string(raw), ".pem") {
		t.Error("worker Caddyfile.tmpl must not reference a plaintext key file")
	}
}
