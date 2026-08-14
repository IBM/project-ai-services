package server

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IAMAccessTokenClaims represents the claims in an IBM Cloud IAM access token
type IAMAccessTokenClaims struct {
	jwt.RegisteredClaims
	IAMID      string   `json:"iam_id"`
	ID         string   `json:"id"`
	RealmID    string   `json:"realmid"`
	Identifier string   `json:"identifier"`
	GivenName  string   `json:"given_name"`
	FamilyName string   `json:"family_name"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Sub        string   `json:"sub"`
	SubType    string   `json:"sub_type"`
	Account    Account  `json:"account"`
	IAM        IAM      `json:"iam"`
	GrantType  string   `json:"grant_type"`
	Scope      string   `json:"scope"`
	ClientID   string   `json:"client_id"`
	ACR        int      `json:"acr"`
	AMR        []string `json:"amr"`
	Authn      Authn    `json:"authn"`
}

// Account represents account information in the token
type Account struct {
	Valid     bool   `json:"valid"`
	BSS       string `json:"bss"`
	IMSUserID string `json:"ims_user_id,omitempty"`
}

// IAM represents IAM-specific information
type IAM struct {
	Accounts []string `json:"accounts,omitempty"`
}

// Authn represents authentication information
type Authn struct {
	Sub     string `json:"sub"`
	IAMId   string `json:"iam_id"`
	SubType string `json:"sub_type"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKSet represents a set of JSON Web Keys
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWTTokenValidator validates IBM Cloud IAM tokens using JWKS
type JWTTokenValidator struct {
	jwksURL    string
	keys       map[string]*rsa.PublicKey
	keysMutex  sync.RWMutex
	httpClient *http.Client
	lastFetch  time.Time
	cacheTTL   time.Duration
}

// NewJWTTokenValidator creates a new JWT token validator
func NewJWTTokenValidator(jwksURL string) (*JWTTokenValidator, error) {
	if jwksURL == "" {
		jwksURL = "https://iam.cloud.ibm.com/identity/keys"
	}

	validator := &JWTTokenValidator{
		jwksURL: jwksURL,
		keys:    make(map[string]*rsa.PublicKey),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 1 * time.Hour, // Cache keys for 1 hour
	}

	// Fetch keys on initialization
	if err := validator.fetchKeys(); err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	return validator, nil
}

// fetchKeys fetches the JWKS from the IAM endpoint
func (v *JWTTokenValidator) fetchKeys() error {
	v.keysMutex.Lock()
	defer v.keysMutex.Unlock()

	// Check if we need to refresh (cache TTL)
	if time.Since(v.lastFetch) < v.cacheTTL && len(v.keys) > 0 {
		return nil
	}

	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks JWKSet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("failed to parse JWKS: %w", err)
	}

	// Convert JWKs to RSA public keys
	newKeys := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" {
			continue
		}

		pubKey, err := jwkToRSAPublicKey(jwk)
		if err != nil {
			continue // Skip invalid keys
		}

		newKeys[jwk.Kid] = pubKey
	}

	if len(newKeys) == 0 {
		return fmt.Errorf("no valid RSA keys found in JWKS")
	}

	v.keys = newKeys
	v.lastFetch = time.Now()

	return nil
}

// GetClaims validates a token and returns its claims
func (v *JWTTokenValidator) GetClaims(tokenString string, validateExp bool) (interface{}, error) {
	// Refresh keys if needed
	if err := v.fetchKeys(); err != nil {
		return nil, fmt.Errorf("failed to refresh keys: %w", err)
	}

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &IAMAccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get the key ID from the token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		// Get the public key for this kid
		v.keysMutex.RLock()
		pubKey, exists := v.keys[kid]
		v.keysMutex.RUnlock()

		if !exists {
			return nil, fmt.Errorf("unknown key ID: %s", kid)
		}

		return pubKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Extract claims
	claims, ok := token.Claims.(*IAMAccessTokenClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	// Validate expiration if requested
	if validateExp {
		if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
			return nil, fmt.Errorf("token has expired")
		}
	}

	return claims, nil
}

// jwkToRSAPublicKey converts a JWK to an RSA public key
// logic reused from IBM IAM library
func jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, err
	}

	// The default exponent is usually 65537, so just compare the
	// base64 for [1,0,1] or [0,1,0,1]
	e := 65537
	if jwk.E != "AQAB" { //&& jwKey.E != "AAEAAQ" {
		// still need to decode the big-endian int

		eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
		e = int((new(big.Int).SetBytes(eBytes)).Uint64())
		if err != nil {
			return nil, err
		}
	}

	RSApk := &rsa.PublicKey{
		N: new(big.Int).SetBytes(n),
		E: e,
	}

	return RSApk, nil
}
