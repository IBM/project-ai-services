// Package utils provides helper functions for connectors.
package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// deriveKey returns a 32-byte AES-256 key derived from the given secret string using SHA-256.
func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))

	return sum[:]
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
// The nonce is prepended to the ciphertext and the result is base64-encoded.
// secret is any non-empty string; a 32-byte AES key is derived from it via SHA-256.
func Encrypt(plaintext string, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret must not be empty")
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends the encrypted ciphertext and authentication tag to nonce.
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decodes a base64-encoded value produced by Encrypt and returns the original plaintext.
// secret is any non-empty string; the same SHA-256-derived key used during Encrypt must be supplied.
func Decrypt(ciphertext string, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret must not be empty")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to base64-decode ciphertext: %w", err)
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, encrypted := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// DecryptSensitiveFields returns a copy of params where every key listed in sensitiveKeys
// has its ciphertext value replaced with the corresponding plaintext.
// Fields that are not in sensitiveKeys are copied as-is.
func DecryptSensitiveFields(params map[string]any, sensitiveKeys map[string]bool, encryptionKey string) (map[string]any, error) {
	if len(sensitiveKeys) == 0 || len(params) == 0 {
		return params, nil
	}

	if encryptionKey == "" {
		return nil, fmt.Errorf("encryption key is not configured (DB_ENCRYPTION_KEY must be set)")
	}

	result := make(map[string]any, len(params))
	for k, v := range params {
		if sensitiveKeys[k] {
			ciphertext, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("sensitive field %q must be a string", k)
			}

			plaintext, err := Decrypt(ciphertext, encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt field %q: %w", k, err)
			}

			result[k] = plaintext
		} else {
			result[k] = v
		}
	}

	return result, nil
}

// StripSensitiveFields returns a copy of metadata with all keys listed in sensitiveFields removed.
// The original map is never mutated. A nil or empty metadata returns an empty map.
func StripSensitiveFields(metadata map[string]any, sensitiveFields map[string]bool) map[string]any {
	result := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if !sensitiveFields[k] {
			result[k] = v
		}
	}

	return result
}

// SensitiveFieldsFromSchema inspects the top-level properties of a JSON Schema map and
// returns the set of property names whose "format" is "password". This allows the set of
// fields that require encryption to be driven by the connector's schema.json rather than
// being hardcoded in each provider implementation.
func SensitiveFieldsFromSchema(schema map[string]any) map[string]bool {
	return schemaFieldsBySection(schema, "format", "password")
}

// UpdatableFieldsFromSchema inspects the top-level properties of a JSON Schema map and
// returns the set of property names whose "ui:section" is "Authentication". These are the
// only fields that may be changed after a datasource is created; structural fields
// (Location, File filters, etc.) are immutable.
func UpdatableFieldsFromSchema(schema map[string]any) map[string]bool {
	return schemaFieldsBySection(schema, "ui:section", "Authentication")
}

// schemaFieldsBySection returns the set of top-level property names from a JSON Schema
// where the given key equals the given value. Used to derive both sensitive fields
// (format=password) and updatable fields (ui:section=Authentication) from the schema.
func schemaFieldsBySection(schema map[string]any, key, value string) map[string]bool {
	result := make(map[string]bool)

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return result
	}

	for name, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := prop[key].(string); ok && v == value {
			result[name] = true
		}
	}

	return result
}
