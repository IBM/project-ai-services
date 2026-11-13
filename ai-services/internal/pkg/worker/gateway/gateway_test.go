package gateway

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20 // 1 MiB

// ──────────────────────────────────────────────────────────────────────────────
// Minimal in-memory WorkerRepository used by gateway tests (no DB needed).
// ──────────────────────────────────────────────────────────────────────────────

type fakeWorkerRepo struct {
	workers map[string]*models.Worker
	byID    map[uuid.UUID]*models.Worker
}

func newFakeWorkerRepo() *fakeWorkerRepo {
	return &fakeWorkerRepo{
		workers: make(map[string]*models.Worker),
		byID:    make(map[uuid.UUID]*models.Worker),
	}
}

func (r *fakeWorkerRepo) Upsert(_ context.Context, w *models.Worker) error {
	if existing, ok := r.workers[w.Name]; ok {
		w.ID = existing.ID
	} else {
		w.ID = uuid.New()
	}
	w.RegisteredAt = time.Now()
	w.UpdatedAt = time.Now()
	cp := *w
	r.workers[w.Name] = &cp
	r.byID[w.ID] = &cp
	return nil
}

func (r *fakeWorkerRepo) Update(_ context.Context, id uuid.UUID, u repository.WorkerUpdate) error {
	w, ok := r.byID[id]
	if !ok {
		return nil
	}
	if u.Status != nil {
		w.Status = *u.Status
	}
	if u.LastHeartbeat != nil {
		w.LastHeartbeat = u.LastHeartbeat
	}
	return nil
}

func (r *fakeWorkerRepo) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	w, ok := r.byID[id]
	if !ok {
		return false, nil
	}
	delete(r.workers, w.Name)
	delete(r.byID, id)
	return true, nil
}

func (r *fakeWorkerRepo) GetCount(_ context.Context, _ *repository.ListWorkersRequest) (int, error) {
	return len(r.workers), nil
}

func (r *fakeWorkerRepo) GetAll(_ context.Context, _ *repository.ListWorkersRequest) ([]models.Worker, error) {
	out := make([]models.Worker, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, *w)
	}
	return out, nil
}

func (r *fakeWorkerRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Worker, error) {
	w, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *fakeWorkerRepo) GetByName(_ context.Context, name string) (*models.Worker, error) {
	w, ok := r.workers[name]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *fakeWorkerRepo) GetApplicationIDsByWorkerIDs(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
}

var _ repository.WorkerRepository = (*fakeWorkerRepo)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────────────────────────────────────

// makeTestCSR generates a minimal ECDSA P-256 CSR for use in gateway tests.
func makeTestCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("makeTestCSR: generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: "test-worker"},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}, key)
	if err != nil {
		t.Fatalf("makeTestCSR: create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
}

// preregister calls registry.Preregister and returns the bootstrap token,
// failing the test on any error.
func preregister(t *testing.T, reg *registry.Registry, workerName string) string {
	t.Helper()
	token, err := reg.Preregister(context.Background(), workerName)
	if err != nil {
		t.Fatalf("Preregister(%q): %v", workerName, err)
	}
	return token
}

// startTestGateway creates an in-process gRPC server backed by bufconn and
// returns a connected client plus a stop func.
func startTestGateway(t *testing.T, reg *registry.Registry) (workerpb.WorkerGatewayClient, func()) {
	t.Helper()

	caKey, caCert, _, err := generateCA()
	if err != nil {
		t.Fatalf("startTestGateway: generate CA: %v", err)
	}

	lis := bufconn.Listen(bufSize)
	gw := &Gateway{registry: reg, caCert: caCert, caKey: caKey}

	gw.grpcServer = grpc.NewServer()
	workerpb.RegisterWorkerGatewayServer(gw.grpcServer, gw)

	go gw.grpcServer.Serve(lis) //nolint:errcheck

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	stop := func() {
		conn.Close() //nolint:errcheck
		gw.grpcServer.Stop()
		lis.Close() //nolint:errcheck
	}

	return workerpb.NewWorkerGatewayClient(conn), stop
}

// ──────────────────────────────────────────────────────────────────────────────
// Register RPC
// ──────────────────────────────────────────────────────────────────────────────

func TestGateway_Register_ValidToken(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())
	token := preregister(t, reg, "worker-1")

	client, stop := startTestGateway(t, reg)
	defer stop()

	_, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	})
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	// Worker must now appear in the in-memory registry.
	if _, ok := reg.Get("worker-1"); !ok {
		t.Error("expected worker-1 to be in registry after Register")
	}
}

func TestGateway_Register_InvalidToken(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	_, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: "bad-token",
	})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestGateway_Register_TokenSingleUse(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())
	token := preregister(t, reg, "worker-1")

	client, stop := startTestGateway(t, reg)
	defer stop()

	// First use succeeds.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	}); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}

	// Second use of the same token must fail.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	}); err == nil {
		t.Fatal("second Register: expected error for reused token")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CommandStream RPC
// ──────────────────────────────────────────────────────────────────────────────

// Worker sends an unregistered name — identifyWorker returns Unauthenticated.
// The server may close the stream before the client's Send completes, so we
// accept EOF on Send as equivalent to receiving an error on Recv.
func TestGateway_CommandStream_UnregisteredWorker(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	stream, err := client.CommandStream(context.Background())
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Identify as an unknown worker — should receive Unauthenticated.
	if err := stream.Send(&workerpb.CommandResult{WorkerName: "nobody"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for unregistered worker")
	}
}

// Worker sends an empty worker_name — same Unauthenticated path as UnregisteredWorker.
func TestGateway_CommandStream_MissingWorkerName(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	stream, err := client.CommandStream(context.Background())
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Send a message with no worker_name — should receive InvalidArgument.
	if err := stream.Send(&workerpb.CommandResult{WorkerName: ""}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for missing worker_name")
	}
}

func TestGateway_CommandStream_CommandDelivered(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-2")

	client, stop := startTestGateway(t, reg)
	defer stop()

	// Register the worker via the RPC.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// First message identifies the worker (heartbeat).
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-2",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	// Give the gateway a moment to register the stream.
	time.Sleep(20 * time.Millisecond)

	// Enqueue a command directly onto the worker's channel.
	entry, ok := reg.Get("worker-2")
	if !ok {
		t.Fatal("expected worker-2 in registry after Register RPC")
	}
	entry.CommandCh <- &workerpb.Command{
		CommandId: "test-cmd-1",
		Type:      workerpb.CommandType_COMMAND_TYPE_LIST_PODS,
	}

	// The stream must deliver the command to the worker client.
	got, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv command: %v", err)
	}
	if got.GetCommandId() != "test-cmd-1" {
		t.Errorf("expected command_id %q, got %q", "test-cmd-1", got.GetCommandId())
	}
}

func TestGateway_CommandStream_ResultRouted(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-3")

	client, stop := startTestGateway(t, reg)
	defer stop()

	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// First message: identify worker.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-3",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// Register a waiter before the worker sends back a result.
	resultCh, err := reg.WaitForResult("worker-3", "cmd-xyz")
	if err != nil {
		t.Fatalf("WaitForResult: %v", err)
	}

	// Worker sends the result.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName: "worker-3",
		CommandId:  "cmd-xyz",
		Success:    true,
	}); err != nil {
		t.Fatalf("Send result: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.GetCommandId() != "cmd-xyz" {
			t.Errorf("expected command_id %q, got %q", "cmd-xyz", res.GetCommandId())
		}
		if !res.GetSuccess() {
			t.Error("expected success=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result to be routed")
	}
}

func TestGateway_CommandStream_Disconnect(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-4")

	client, stop := startTestGateway(t, reg)
	defer stop()

	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    "podman",
		CsrPem:         makeTestCSR(t),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Identify the worker.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-4",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := reg.Get("worker-4"); !ok {
		t.Fatal("expected worker-4 in registry after opening stream")
	}

	// Cancel the client-side context to simulate the worker disconnecting.
	cancel()

	// Give the gateway time to process the disconnect.
	time.Sleep(50 * time.Millisecond)

	if _, ok := reg.Get("worker-4"); ok {
		t.Error("expected worker-4 to be removed from registry after disconnect")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// PKI / SAN tests
// ──────────────────────────────────────────────────────────────────────────────

func TestGenerateServerCert_MultiSAN(t *testing.T) {
	caKey, caCert, _, err := generateCA()
	if err != nil {
		t.Fatalf("generateCA: %v", err)
	}

	sans := []string{"catalog-worker-gateway.example.com"}
	_, certDER, err := generateServerCert(caCert, caKey, sans)
	if err != nil {
		t.Fatalf("generateServerCert: %v", err)
	}

	parsed, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	got := make(map[string]bool, len(parsed.DNSNames))
	for _, n := range parsed.DNSNames {
		got[n] = true
	}
	for _, want := range sans {
		if !got[want] {
			t.Errorf("expected SAN %q in cert, got DNSNames=%v", want, parsed.DNSNames)
		}
	}
}

func TestGenerateAndPersistPKI_PodmanSANs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv(workerconstants.MTLSEncryptionKeyEnv, "test-mtls-secret")

	res, err := generateAndPersistPKI(t.Context(), dir, "podman")
	if err != nil {
		t.Fatalf("generateAndPersistPKI: %v", err)
	}

	leaf, err := x509.ParseCertificate(res.serverCert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	expectedSANs := []string{"catalog-worker-gateway.example.com", workerconstants.PodmanGatewayPodName, "example.com"}
	if !slices.Equal(leaf.DNSNames, expectedSANs) {
		t.Errorf("expected SANs %v; got DNSNames=%v", expectedSANs, leaf.DNSNames)
	}
}

// TestGenerateAndPersistPKI_NoPlaintextKeyOnDisk checks that the server key is only
// ever stored encrypted, and that only the server key (not the CA signing key) is
// made readable for the Caddy user.
func TestGenerateAndPersistPKI_NoPlaintextKeyOnDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv(workerconstants.MTLSEncryptionKeyEnv, "test-mtls-secret")

	if _, err := loadOrGeneratePKI(t.Context(), dir, "podman"); err != nil {
		t.Fatalf("loadOrGeneratePKI: %v", err)
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
		if bytes.Contains(data, []byte("PRIVATE KEY")) {
			t.Errorf("%s holds a plaintext private key; none may be written to disk", e.Name())
		}
	}

	assertCaddyReadablePKI(t, dir)
}

func assertCaddyReadablePKI(t *testing.T, dir string) {
	t.Helper()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if dirInfo.Mode().Perm()&0o055 != 0o055 {
		t.Errorf("dir mode %v: must be traversable by the Caddy user", dirInfo.Mode().Perm())
	}

	srvInfo, err := os.Stat(filepath.Join(dir, "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	if srvInfo.Mode().Perm()&0o044 != 0o044 {
		t.Errorf("server.key mode %v: the encrypted key must be readable by the Caddy user", srvInfo.Mode().Perm())
	}

	caInfo, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if caInfo.Mode().Perm()&0o077 != 0 {
		t.Errorf("ca.key mode %v: the CA signing key must stay private to its owner", caInfo.Mode().Perm())
	}
}

func TestGenerateServerCert_ServerAndClientAuth(t *testing.T) {
	caKey, caCert, _, err := generateCA()
	if err != nil {
		t.Fatalf("generateCA: %v", err)
	}
	_, certDER, err := generateServerCert(caCert, caKey, []string{"cp.example.com"})
	if err != nil {
		t.Fatalf("generateServerCert: %v", err)
	}
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	assertVerifiesFor(t, leaf, caCert, x509.ExtKeyUsageServerAuth, "cp.example.com")
	assertVerifiesFor(t, leaf, caCert, x509.ExtKeyUsageClientAuth, "")
}

func TestSignWorkerCSR_ServerAndClientAuthWithSAN(t *testing.T) {
	caKey, caCert, _, err := generateCA()
	if err != nil {
		t.Fatalf("generateCA: %v", err)
	}

	sans := []string{"w1.example.com"}
	certPEM, _, _, err := signWorkerCSR(makeTestCSR(t), "w1", sans, caCert, caKey)
	if err != nil {
		t.Fatalf("signWorkerCSR: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	if leaf.Subject.CommonName != "w1" {
		t.Errorf("CN = %q, want w1 (the worker recovers its name from it)", leaf.Subject.CommonName)
	}
	assertVerifiesFor(t, leaf, caCert, x509.ExtKeyUsageServerAuth, "w1.example.com")
	assertVerifiesFor(t, leaf, caCert, x509.ExtKeyUsageClientAuth, "")
}

// TestSignWorkerCSR_IgnoresCSRSANs guards the trust boundary: the gateway decides
// the SANs, so a worker cannot request another node's name in its CSR.
func TestSignWorkerCSR_IgnoresCSRSANs(t *testing.T) {
	caKey, caCert, _, err := generateCA()
	if err != nil {
		t.Fatalf("generateCA: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{Organization: []string{"system:workers"}},
		DNSNames: []string{"control-plane.example.com"},
	}, key)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	certPEM, _, _, err := signWorkerCSR(csrPEM, "w1", []string{"w1.example.com"}, caCert, caKey)
	if err != nil {
		t.Fatalf("signWorkerCSR: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	if !slices.Equal(leaf.DNSNames, []string{"w1.example.com"}) {
		t.Errorf("DNSNames = %v, want only the gateway-chosen SAN", leaf.DNSNames)
	}
}

func TestWorkerCertDNSNames(t *testing.T) {
	meta := map[string]string{workerconstants.MetaKeyDomainSuffix: "10.0.0.5.nip.io"}

	tests := []struct {
		name    string
		runtime types.RuntimeType
		meta    map[string]string
		want    []string
	}{
		{"podman with domain suffix", types.RuntimeTypePodman, meta, []string{"w1.10.0.0.5.nip.io"}},
		{"podman without domain suffix", types.RuntimeTypePodman, map[string]string{}, nil},
		{"podman nil metadata", types.RuntimeTypePodman, nil, nil},
		{"openshift is not supported yet", types.RuntimeTypeOpenShift, meta, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := workerCertDNSNames("w1", tc.runtime, tc.meta)
			if !slices.Equal(got, tc.want) {
				t.Errorf("workerCertDNSNames = %v, want %v", got, tc.want)
			}
		})
	}
}

// assertVerifiesFor fails unless leaf chains to ca for the given usage and, when
// host is non-empty, is valid for that hostname.
func assertVerifiesFor(t *testing.T, leaf, ca *x509.Certificate, usage x509.ExtKeyUsage, host string) {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{usage}, DNSName: host}); err != nil {
		t.Errorf("cert does not verify for usage %v host %q: %v", usage, host, err)
	}
}

// TestCatalogCaddyfile_PrivateIngressUsesGatewayPKI keeps the Caddyfile paths in
// step with the file names the gateway writes.
func TestCatalogCaddyfile_PrivateIngressUsesGatewayPKI(t *testing.T) {
	raw, err := assets.CatalogFS.ReadFile("catalog/podman/Caddyfile.tmpl")
	if err != nil {
		t.Fatalf("read catalog Caddyfile: %v", err)
	}

	want := []string{
		"get_certificate encrypted_file",
		"cert        " + filepath.Join(workerconstants.GatewayPKIDir, "server.crt"),
		"key         " + filepath.Join(workerconstants.GatewayPKIDir, "server.key"),
		"secret_file /etc/secret/catalog-mtls-encryption-secret/mtls-encryption-key",
		"trusted_ca_cert_file " + filepath.Join(workerconstants.GatewayPKIDir, "ca.crt"),
	}
	for _, s := range want {
		if !strings.Contains(string(raw), s) {
			t.Errorf("catalog Caddyfile.tmpl does not reference %q", s)
		}
	}
	if strings.Contains(string(raw), ".pem") {
		t.Error("catalog Caddyfile.tmpl must not reference a plaintext key file")
	}
}

func TestGenerateAndPersistPKI_PodmanNoDomain(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "")

	_, err := generateAndPersistPKI(t.Context(), t.TempDir(), "podman")
	if err == nil {
		t.Fatal("expected error when DOMAIN_SUFFIX is unset, got nil")
	}
}

func TestGenerateAndPersistPKI_UnknownRuntime(t *testing.T) {
	dir := t.TempDir()

	_, err := generateAndPersistPKI(t.Context(), dir, "unknown")
	if err == nil {
		t.Fatal("expected error for unsupported runtime type, got nil")
	}
}
