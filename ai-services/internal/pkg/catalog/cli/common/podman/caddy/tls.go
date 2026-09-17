package caddy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	httpsReadyPollInterval   = 2 * time.Second
	httpsReadyTimeout        = 60 * time.Second
	httpsReadyRequestTimeout = 5 * time.Second
)

// WaitForTLSReady polls rawURL via TLS handshake until Caddy finishes reloading TLS for the domain.
func WaitForTLSReady(ctx context.Context, rawURL string) error {
	logger.Debugf("Waiting for Caddy TLS to be ready at %s...\n", rawURL)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %s: %w", rawURL, err)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	addr := net.JoinHostPort(host, port)

	dialer := &net.Dialer{Timeout: httpsReadyRequestTimeout}
	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // self-signed certs are expected
	}

	deadline := time.Now().Add(httpsReadyTimeout)
	ticker := time.NewTicker(httpsReadyPollInterval)
	defer ticker.Stop()

	for {
		conn, dialErr := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if dialErr == nil {
			_ = conn.Close()
			logger.Debugf("Caddy TLS ready at %s\n", addr)

			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Caddy TLS at %s: %w", addr, dialErr)
		}

		logger.Debugf("Caddy TLS not ready at %s (%v), retrying in %s...\n", addr, dialErr, httpsReadyPollInterval)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
