package miq_test

import "crypto/tls"

// cryptoTLSConfig returns a *tls.Config used by integration test helpers.
func cryptoTLSConfig(insecure bool) *tls.Config {
	return &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec
}
