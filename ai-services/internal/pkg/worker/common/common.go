package common

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// IsPodmanLocalWorker returns true when the catalog pod on this Podman node has
// LOCAL_WORKER=true in any of its container env vars.
func IsPodmanLocalWorker(ctx context.Context, rt runtime.Runtime) (bool, error) {
	pod, err := rt.InspectPod(ctx, workerconstants.PodmanGatewayPodName)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspect catalog pod: %w", err)
	}

	for _, container := range pod.Containers {
		cInfo, err := rt.InspectContainer(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("inspect container %s: %w", container.ID, err)
		}
		if cInfo.Env[workerconstants.LocalWorkerEnvVar] == "true" {
			return true, nil
		}
	}

	return false, nil
}

// WorkerNameFromCert reads the CN from the worker's client certificate in tlsDir.
// This recovers the registered worker name without any extra state file,
// because the gateway embeds the token-bound worker name as the cert CN at registration time.
// tls.crt is public material and stored in plaintext — no decryption needed here.
func WorkerNameFromCert(tlsDir string) (string, error) {
	certPEM, err := os.ReadFile(filepath.Join(tlsDir, "tls.crt"))
	if err != nil {
		return "", fmt.Errorf("read tls.crt: %w", err)
	}

	return workerNameFromCertPEM([]byte(certPEM))
}

// ResolveWorkerName returns the name this node is registered under in the
// catalog by exec-ing into the running worker pod and reading the CN from its
// mTLS certificate.
// Works on both Podman and OpenShift runtimes.
func ResolveWorkerName(ctx context.Context, rt runtime.Runtime) (string, error) {
	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {workerconstants.WorkerPodLabel},
	})
	if err != nil {
		return "", fmt.Errorf("list worker pods: %w", err)
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("no worker pods found")
	}

	certPEM, err := rt.ExecInContainerWithCmd(ctx, pods[0].Name, "worker",
		[]string{"cat", workerconstants.WorkerTLSDir + "/tls.crt"})
	if err != nil {
		return "", fmt.Errorf("read worker tls.crt from container: %w", err)
	}

	name, err := workerNameFromCertPEM([]byte(strings.TrimSpace(certPEM)))
	if err != nil {
		return "", fmt.Errorf("extract worker name from tls.crt: %w", err)
	}

	return name, nil
}

// workerNameFromCertPEM parses a PEM-encoded certificate and returns the CN.
func workerNameFromCertPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("tls.crt: not valid PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse tls.crt: %w", err)
	}

	if cert.Subject.CommonName == "" {
		return "", fmt.Errorf("tls.crt: CN is empty")
	}

	return cert.Subject.CommonName, nil
}

// IsOpenShiftLocalWorker returns true when any catalog-backend pod in the
// namespace has LOCAL_WORKER=true in its pod spec env vars.
func IsOpenShiftLocalWorker(ctx context.Context, rt runtime.Runtime) (bool, error) {
	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {workerconstants.CatalogBackendPodLabel + "=" + workerconstants.CatalogBackendPodLabelValue},
	})
	if err != nil {
		return false, fmt.Errorf("list catalog-backend pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Env[workerconstants.LocalWorkerEnvVar] == "true" {
			return true, nil
		}
	}

	return false, nil
}
