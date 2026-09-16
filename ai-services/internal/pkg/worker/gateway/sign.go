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
)

// signWorkerCSR validates and signs a PEM-encoded CSR using the gateway CA,
// returning the signed certificate and CA certificate as PEM bytes together with
// the cert's expiry time.
//
// workerName is embedded as the cert CN so the worker can recover its registered
// name from the cert on reconnect without any additional state file.
func signWorkerCSR(csrPEM []byte, workerName string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, caCertPEM []byte, notAfter time.Time, err error) {
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
		NotBefore:    time.Now(),
		NotAfter:     notAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	signedDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("sign certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signedDER})
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	return certPEM, caCertPEM, notAfter, nil
}
