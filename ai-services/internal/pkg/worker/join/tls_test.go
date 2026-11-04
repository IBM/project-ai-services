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

func TestWriteTLSMaterial_WritesPlaintextKeyForCaddy(t *testing.T) {
	dir := writeTestCredentials(t)

	plain, err := os.ReadFile(filepath.Join(dir, tlsKeyPlaintextFile))
	if err != nil {
		t.Fatalf("read %s: %v", tlsKeyPlaintextFile, err)
	}
	if !strings.Contains(string(plain), "PRIVATE KEY") {
		t.Errorf("%s must hold a plaintext PEM key", tlsKeyPlaintextFile)
	}

	enc, err := os.ReadFile(filepath.Join(dir, tlsKeyFile))
	if err != nil {
		t.Fatalf("read %s: %v", tlsKeyFile, err)
	}
	if strings.Contains(string(enc), "PRIVATE KEY") {
		t.Errorf("%s must stay encrypted at rest", tlsKeyFile)
	}

	info, err := os.Stat(filepath.Join(dir, tlsKeyPlaintextFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o044 != 0o044 {
		t.Errorf("%s mode %v: must be group/other readable for the Caddy user", tlsKeyPlaintextFile, info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm()&0o055 != 0o055 {
		t.Errorf("dir mode %v: must be traversable by the Caddy user", dirInfo.Mode().Perm())
	}
}

// TestLoadClientCert_RefreshesPlaintextKey covers worker restarts: the worker
// re-decrypts tls.key on every start, so the Caddy copy is restored if it is gone.
func TestLoadClientCert_RefreshesPlaintextKey(t *testing.T) {
	dir := writeTestCredentials(t)
	if err := os.Remove(filepath.Join(dir, tlsKeyPlaintextFile)); err != nil {
		t.Fatal(err)
	}

	if _, err := loadClientCert(dir); err != nil {
		t.Fatalf("loadClientCert: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, tlsKeyPlaintextFile)); err != nil {
		t.Errorf("%s was not restored: %v", tlsKeyPlaintextFile, err)
	}
}

// TestWorkerCaddyfile_PrivateIngressUsesWorkerTLSFiles keeps the Caddyfile paths in
// step with the file names the worker writes.
func TestWorkerCaddyfile_PrivateIngressUsesWorkerTLSFiles(t *testing.T) {
	raw, err := assets.WorkerFS.ReadFile("worker/podman/Caddyfile.tmpl")
	if err != nil {
		t.Fatalf("read worker Caddyfile: %v", err)
	}

	want := []string{
		filepath.Join(workerconstants.WorkerTLSDir, tlsCertFile),
		filepath.Join(workerconstants.WorkerTLSDir, tlsKeyPlaintextFile),
		"trusted_ca_cert_file " + filepath.Join(workerconstants.WorkerTLSDir, caCertFile),
	}
	for _, s := range want {
		if !strings.Contains(string(raw), s) {
			t.Errorf("worker Caddyfile.tmpl does not reference %q", s)
		}
	}
}
