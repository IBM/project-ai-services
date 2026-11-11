// Package encryptedfile adds the tls.get_certificate.encrypted_file Caddy module.
//
// It serves a certificate whose private key is stored on disk AES-256-GCM
// encrypted, in the format written by ai-services (base64(nonce[12] || ciphertext),
// AES key = SHA-256(secret)). Caddy itself cannot read such a key, so this module
// decrypts it in memory when a TLS handshake needs it. No plaintext key is
// written anywhere.
//
// The files are re-read whenever their modification time or size changes, so a
// rotated certificate is served from the next handshake with no Caddy reload.
//
// Caddyfile usage (inside a tls block):
//
//	get_certificate encrypted_file {
//	    cert        /data/worker-tls/tls.crt
//	    key         /data/worker-tls/tls.key
//	    secret_file /etc/secret/worker-mtls-encryption-secret/mtls-encryption-key
//	}
package encryptedfile

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"go.uber.org/zap"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func init() {
	caddy.RegisterModule(&EncryptedFile{})
}

// fileStamp identifies a version of a file on disk.
type fileStamp struct {
	modTime time.Time
	size    int64
}

func stampOf(path string) (fileStamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}

	return fileStamp{modTime: info.ModTime(), size: info.Size()}, nil
}

// EncryptedFile is a certificate manager that loads a certificate and an
// encrypted private key from disk.
type EncryptedFile struct {
	// CertFile is the path of the PEM certificate (chain).
	CertFile string `json:"cert_file,omitempty"`
	// KeyFile is the path of the encrypted private key.
	KeyFile string `json:"key_file,omitempty"`
	// SecretFile is the path of a file holding the decryption secret.
	// Trailing newlines are ignored.
	SecretFile string `json:"secret_file,omitempty"`

	mu     sync.Mutex
	cached *tls.Certificate
	stamps [3]fileStamp // cert, key, secret
	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (*EncryptedFile) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "tls.get_certificate.encrypted_file",
		New: func() caddy.Module { return new(EncryptedFile) },
	}
}

// Provision validates the configuration.
func (e *EncryptedFile) Provision(ctx caddy.Context) error {
	e.logger = ctx.Logger()

	if e.CertFile == "" || e.KeyFile == "" || e.SecretFile == "" {
		return errors.New("encrypted_file: cert, key and secret_file are all required")
	}

	return nil
}

// GetCertificate is called by Caddy on a TLS handshake. It returns (nil, nil)
// for names the certificate does not cover, so other sites served by the same
// Caddy are not affected.
func (e *EncryptedFile) GetCertificate(_ context.Context, hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert, err := e.current()
	if err != nil {
		return nil, err
	}

	if !covers(cert, hello.ServerName) {
		return nil, nil //nolint:nilnil // (nil, nil) tells Caddy this manager has no certificate for the name
	}

	return cert, nil
}

// current returns the parsed certificate, reloading it when any input file changed.
func (e *EncryptedFile) current() (*tls.Certificate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	paths := [3]string{e.CertFile, e.KeyFile, e.SecretFile}
	var stamps [3]fileStamp
	for i, p := range paths {
		s, err := stampOf(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("encrypted_file: %s is not available yet: %w", p, err)
			}

			return nil, fmt.Errorf("encrypted_file: %w", err)
		}
		stamps[i] = s
	}

	if e.cached != nil && stamps == e.stamps {
		return e.cached, nil
	}

	cert, err := loadCertificate(e.CertFile, e.KeyFile, e.SecretFile)
	if err != nil {
		return nil, err
	}

	e.cached, e.stamps = cert, stamps
	if e.logger != nil {
		e.logger.Info("loaded certificate", zap.String("cert_file", e.CertFile))
	}

	return cert, nil
}

func loadCertificate(certFile, keyFile, secretFile string) (*tls.Certificate, error) {
	secretRaw, err := os.ReadFile(secretFile)
	if err != nil {
		return nil, fmt.Errorf("encrypted_file: read secret: %w", err)
	}
	secret := strings.TrimRight(string(secretRaw), "\r\n")

	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("encrypted_file: read certificate: %w", err)
	}

	keyEnc, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("encrypted_file: read key: %w", err)
	}

	keyPEM, err := decrypt(strings.TrimSpace(string(keyEnc)), secret)
	if err != nil {
		return nil, fmt.Errorf("encrypted_file: decrypt %s: %w", keyFile, err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("encrypted_file: %w", err)
	}

	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return nil, fmt.Errorf("encrypted_file: parse certificate: %w", err)
		}
	}

	return &cert, nil
}

// covers reports whether the certificate is valid for the requested server name.
func covers(cert *tls.Certificate, serverName string) bool {
	return serverName != "" && cert.Leaf.VerifyHostname(serverName) == nil
}

// decrypt reverses ai-services' catalog/utils.Encrypt.
func decrypt(encoded, secret string) ([]byte, error) {
	if secret == "" {
		return nil, errors.New("secret must not be empty")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64-decode: %w", err)
	}

	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plain, nil
}

// UnmarshalCaddyfile parses:
//
//	encrypted_file {
//	    cert        <path>
//	    key         <path>
//	    secret_file <path>
//	}
func (e *EncryptedFile) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume the module name

	if d.NextArg() {
		return d.ArgErr()
	}

	for nesting := d.Nesting(); d.NextBlock(nesting); {
		var target *string

		switch d.Val() {
		case "cert":
			target = &e.CertFile
		case "key":
			target = &e.KeyFile
		case "secret_file":
			target = &e.SecretFile
		default:
			return d.Errf("unknown subdirective %q", d.Val())
		}

		if !d.NextArg() {
			return d.ArgErr()
		}
		*target = d.Val()

		if d.NextArg() {
			return d.ArgErr()
		}
	}

	return nil
}

// Interface guards.
var (
	_ certmagic.Manager     = (*EncryptedFile)(nil)
	_ caddy.Module          = (*EncryptedFile)(nil)
	_ caddy.Provisioner     = (*EncryptedFile)(nil)
	_ caddyfile.Unmarshaler = (*EncryptedFile)(nil)
)
