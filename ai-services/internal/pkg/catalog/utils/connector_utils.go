package utils

import (
	"fmt"
)

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
