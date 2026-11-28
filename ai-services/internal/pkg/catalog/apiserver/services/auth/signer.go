package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
)

const (
	// ServiceTokenIssuer is the `iss` claim of service tokens. It matches the catalog's own tokens.
	ServiceTokenIssuer = "ai-services-catalog-server"
	// DefaultServiceTokenTTL is the default lifetime of a service token. It equals the ceiling because the chat UI
	// cannot renew a token without a full-page redirect that loses the conversation (DA5).
	DefaultServiceTokenTTL = time.Hour
	// MaxServiceTokenTTL is the ceiling for --service-token-ttl.
	MaxServiceTokenTTL = time.Hour

	// jwksRetiredGrace keeps a retired key in the JWKS long enough for tokens signed with it to expire.
	jwksRetiredGrace = time.Hour
	kidBytes         = 16
	jtiBytes         = 16
	coordinateBytes  = 32 // P-256
	jwtTypeUser      = "user"
	jwtTypeService   = "service"

	// InternalSubject is the `sub` of tokens the catalog issues to itself.
	InternalSubject = "catalog"
)

// ServiceClaims are the claims of a service token (design doc section 5.4).
type ServiceClaims struct {
	Scope string `json:"scope"`
	IDP   string `json:"idp"`
	Type  string `json:"typ"`
	jwt.RegisteredClaims
}

// JWK is one public key in a JSON Web Key Set.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// JWKS is a JSON Web Key Set. It never contains private members.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// Signer signs service tokens with the catalog's ES256 key, which lives encrypted in the database.
type Signer struct {
	repo   dbrepo.SigningKeyRepository
	kid    string
	priv   *ecdsa.PrivateKey
	secret string
}

// NewSigner loads the active signing key, creating it on first start. encryptionKey is
// DB_ENCRYPTION_KEY and must not be empty. Concurrent replicas end up with the same key.
func NewSigner(ctx context.Context, repo dbrepo.SigningKeyRepository, encryptionKey string) (*Signer, error) {
	if encryptionKey == "" {
		return nil, errors.New("signing key store requires DB_ENCRYPTION_KEY")
	}

	rec, err := repo.EnsureActive(ctx, func() (*dbmodels.SigningKey, error) { return generateSigningKey(encryptionKey) })
	if err != nil {
		return nil, err
	}

	pemText, err := catalogutils.Decrypt(rec.PrivateKeyEnc, encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt signing key %s (was DB_ENCRYPTION_KEY changed?): %w", rec.KID, err)
	}

	priv, err := parsePrivateKeyPEM(pemText)
	if err != nil {
		return nil, fmt.Errorf("signing key %s: %w", rec.KID, err)
	}

	return &Signer{repo: repo, kid: rec.KID, priv: priv, secret: encryptionKey}, nil
}

func generateSigningKey(encryptionKey string) (*dbmodels.SigningKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing key: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to encode private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encode public key: %w", err)
	}

	enc, err := catalogutils.Encrypt(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})), encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt private key: %w", err)
	}

	kid, err := randomHex(kidBytes)
	if err != nil {
		return nil, err
	}

	return &dbmodels.SigningKey{
		KID:           kid,
		PrivateKeyEnc: enc,
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
	}, nil
}

func parsePrivateKeyPEM(text string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("no PEM block in private key")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := k.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return nil, errors.New("signing key is not an ECDSA P-256 key")
	}

	return priv, nil
}

// Sign signs claims with the active key (ES256, `kid` header set).
func (s *Signer) Sign(claims ServiceClaims) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = s.kid

	return t.SignedString(s.priv)
}

// JWKS returns the public keys that verifiers may need: every active key plus recently retired ones.
func (s *Signer) JWKS(ctx context.Context) (*JWKS, error) {
	recs, err := s.repo.ListPublishable(ctx, jwksRetiredGrace)
	if err != nil {
		return nil, err
	}

	set := &JWKS{Keys: make([]JWK, 0, len(recs))}
	for _, r := range recs {
		jwk, err := publicJWK(r.KID, r.PublicKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("signing key %s: %w", r.KID, err)
		}
		set.Keys = append(set.Keys, *jwk)
	}

	return set, nil
}

func publicJWK(kid, pubPEM string) (*JWK, error) {
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return nil, errors.New("no PEM block in public key")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("public key is not ECDSA P-256")
	}

	// Uncompressed point: 0x04 || X || Y, each coordinate coordinateBytes long.
	raw, err := pub.Bytes()
	if err != nil || len(raw) != 1+2*coordinateBytes {
		return nil, errors.New("unexpected public key encoding")
	}

	return &JWK{
		Kty: "EC", Crv: "P-256", Use: "sig", Alg: "ES256", Kid: kid,
		X: base64.RawURLEncoding.EncodeToString(raw[1 : 1+coordinateBytes]),
		Y: base64.RawURLEncoding.EncodeToString(raw[1+coordinateBytes:]),
	}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}
