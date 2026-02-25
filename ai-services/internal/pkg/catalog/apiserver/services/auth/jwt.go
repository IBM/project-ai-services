package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

type customClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func (t *TokenManager) newToken(uid string, ttl time.Duration, tokenType string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := customClaims{
		UserID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "ai-services-catalog-server",
			Subject:   uid,
			Audience:  []string{tokenType},
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(t.secret)

	return signed, exp, err
}

func (t *TokenManager) GenerateAccessToken(uid string) (string, time.Time, error) {
	return t.newToken(uid, t.accessTTL, "access")
}

func (t *TokenManager) GenerateRefreshToken(uid string) (string, time.Time, error) {
	return t.newToken(uid, t.refreshTTL, "refresh")
}

func (t *TokenManager) ValidateAccessToken(raw string) (string, time.Time, error) {
	claims, err := t.parse(raw)
	if err != nil {
		return "", time.Time{}, err
	}
	if !contains(claims.Audience, "access") {
		return "", time.Time{}, errors.New("not an access token")
	}

	return claims.UserID, claims.ExpiresAt.Time, nil
}

func (t *TokenManager) ValidateRefreshToken(raw string) (string, time.Time, error) {
	claims, err := t.parse(raw)
	if err != nil {
		return "", time.Time{}, err
	}
	if !contains(claims.Audience, "refresh") {
		return "", time.Time{}, errors.New("not a refresh token")
	}

	return claims.UserID, claims.ExpiresAt.Time, nil
}

func (t *TokenManager) parse(raw string) (*customClaims, error) {
	token, err := jwt.ParseWithClaims(raw, &customClaims{}, func(token *jwt.Token) (interface{}, error) {
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := token.Claims.(*customClaims)
	if !ok {
		return nil, errors.New("claims cast error")
	}

	return claims, nil
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}

	return false
}
