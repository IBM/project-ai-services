package encryptedfile

import (
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

func init() {
	caddy.RegisterModule(&EncryptedMTLSTransport{})
}

// EncryptedMTLSTransport is the reverse_proxy HTTP transport used for private
// egress. It behaves exactly like the stock "http" transport, but the client
// certificate presented to the upstream is loaded through the same
// AES-256-GCM encrypted key handling as the encrypted_file certificate
// manager, so the client key is never written to disk in plaintext.
//
// The stock transport cannot do this: client_certificate_key_file must be a
// plaintext PEM key.
//
// The certificate is re-read when any of the three files changes, so a rotated
// certificate is used from the next TLS handshake with no Caddy reload.
//
// JSON usage (the "tls" block is the stock transport TLS block and still
// supplies the trusted CA and server_name):
//
//	{
//	  "protocol": "encrypted_mtls",
//	  "client_cert_file": "/data/worker-tls/tls.crt",
//	  "encrypted_client_key_file": "/data/worker-tls/tls.key",
//	  "secret_file": "/etc/secret/worker-mtls-encryption-secret/mtls-encryption-key",
//	  "tls": {"ca": {"provider": "file", "pem_files": ["/data/worker-tls/ca.crt"]}}
//	}
type EncryptedMTLSTransport struct {
	reverseproxy.HTTPTransport

	// ClientCertFile is the path of the PEM client certificate (chain).
	ClientCertFile string `json:"client_cert_file,omitempty"`
	// EncryptedClientKeyFile is the path of the encrypted client private key.
	EncryptedClientKeyFile string `json:"encrypted_client_key_file,omitempty"`
	// SecretFile is the path of a file holding the decryption secret.
	SecretFile string `json:"secret_file,omitempty"`

	loader *EncryptedFile
}

// CaddyModule returns the Caddy module information.
func (*EncryptedMTLSTransport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.transport.encrypted_mtls",
		New: func() caddy.Module { return new(EncryptedMTLSTransport) },
	}
}

// Provision builds the stock transport, then points its TLS client config at the
// encrypted certificate loader.
func (t *EncryptedMTLSTransport) Provision(ctx caddy.Context) error {
	if t.ClientCertFile == "" || t.EncryptedClientKeyFile == "" || t.SecretFile == "" {
		return errors.New("encrypted_mtls: client_cert_file, encrypted_client_key_file and secret_file are all required")
	}

	if t.TLS == nil {
		return errors.New("encrypted_mtls: a tls block is required (it supplies the trusted CA)")
	}

	if t.TLS.ClientCertificateFile != "" || t.TLS.ClientCertificateKeyFile != "" || t.TLS.ClientCertificateAutomate != "" {
		return errors.New("encrypted_mtls: do not set client certificate fields inside the tls block")
	}

	if err := t.HTTPTransport.Provision(ctx); err != nil {
		return err
	}

	if t.Transport == nil || t.Transport.TLSClientConfig == nil {
		return errors.New("encrypted_mtls: transport has no TLS client config")
	}

	t.loader = &EncryptedFile{CertFile: t.ClientCertFile, KeyFile: t.EncryptedClientKeyFile, SecretFile: t.SecretFile}
	t.loader.logger = ctx.Logger()

	// The config pointer is shared with the transport, which clones it per
	// dial, so setting the hook after construction takes effect.
	t.Transport.TLSClientConfig.GetClientCertificate = t.clientCertificate

	return nil
}

// clientCertificate returns the current client certificate for a handshake.
func (t *EncryptedMTLSTransport) clientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	cert, err := t.loader.current()
	if err != nil {
		return nil, fmt.Errorf("encrypted_mtls: %w", err)
	}

	return cert, nil
}

// Interface guards.
var (
	_ caddy.Module      = (*EncryptedMTLSTransport)(nil)
	_ caddy.Provisioner = (*EncryptedMTLSTransport)(nil)
)
