package models

import "time"

// SigningKey is a catalog service-token signing key.
// PrivateKeyEnc is the AES-256-GCM encrypted PEM of the private key; it must never
// be logged or returned by an API.
type SigningKey struct {
	KID           string
	PrivateKeyEnc string
	PublicKeyPEM  string
	CreatedAt     time.Time
	RetiredAt     *time.Time
}

// AuthCode is a one-time authorization code row. Only the hash of the code is stored.
type AuthCode struct {
	CodeHash      string
	UserID        string
	IDP           string
	Service       string
	RedirectURI   string
	CodeChallenge string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UsedAt        *time.Time
}
