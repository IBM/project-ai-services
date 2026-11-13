package gateway

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// workerCertDNSNames returns the DNS SANs the gateway embeds in a worker's
// certificate. They are derived from the token-bound worker name and the
// worker's reported domain suffix, never from the CSR, so a worker cannot claim
// another node's identity. Go TLS ignores the CN, so the SAN must cover the
// hostname the control-plane Caddy uses as ServerName when dialing the worker's
// private ingress.
func workerCertDNSNames(workerName string, runtimeType types.RuntimeType, meta map[string]string) []string {
	if runtimeType != types.RuntimeTypePodman {
		return nil
	}

	domainSuffix := meta[workerconstants.MetaKeyDomainSuffix]
	if domainSuffix == "" {
		return nil
	}

	return []string{workerName + "." + domainSuffix}
}

// signWorkerCSR validates and signs a PEM-encoded CSR using the gateway CA,
// returning the signed certificate and CA certificate as PEM bytes together with
// the cert's expiry time.
//
// workerName is embedded as the cert CN so the worker can recover its registered
// name from the cert on reconnect without any additional state file.
//
// dnsNames are the DNS SANs to embed (see workerCertDNSNames).
func signWorkerCSR(csrPEM []byte, workerName string, dnsNames []string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, caCertPEM []byte, notAfter time.Time, err error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, nil, time.Time{}, fmt.Errorf("malformed CSR: not a valid PEM CERTIFICATE REQUEST block")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("invalid CSR signature: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBitSize))
	notAfter = time.Now().Add(workerCertTTL)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: workerName, Organization: csr.Subject.Organization},
		DNSNames:     dnsNames,
		NotBefore:    time.Now(),
		NotAfter:     notAfter,
		// ClientAuth: the gRPC stream to the gateway, and the worker Caddy dialing the
		//            control-plane private ingress.
		// ServerAuth: the worker Caddy serves this cert on its own private ingress.
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}
	signedDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("sign certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signedDER})
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	return certPEM, caCertPEM, notAfter, nil
}
