package utils

import (
	"reflect"
	"testing"
)

func TestEncryptSensitiveFields_EmptyCases(t *testing.T) {
	params := map[string]any{"user": "alice", "password": "secret"}
	sensitive := map[string]bool{"password": true}

	// Empty params
	out, err := EncryptSensitiveFields(nil, sensitive, "key")
	if err != nil || out != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", out, err)
	}

	// Empty sensitive keys
	out, err = EncryptSensitiveFields(params, nil, "key")
	if err != nil || !reflect.DeepEqual(out, params) {
		t.Fatalf("expected (%v, nil), got (%v, %v)", params, out, err)
	}

	// Empty key with non-empty sensitive keys
	_, err = EncryptSensitiveFields(params, sensitive, "")
	if err == nil {
		t.Fatal("expected error for empty encryption key, got nil")
	}
}

func TestEncryptSensitiveFields_NonStringSensitiveField(t *testing.T) {
	params := map[string]any{"password": 12345}
	sensitive := map[string]bool{"password": true}

	_, err := EncryptSensitiveFields(params, sensitive, "key")
	if err == nil {
		t.Fatal("expected error when sensitive field is not a string, got nil")
	}
}

func TestDecryptSensitiveFields_EmptyCases(t *testing.T) {
	params := map[string]any{"user": "alice", "password": "secret"}
	sensitive := map[string]bool{"password": true}

	// Empty params
	out, err := DecryptSensitiveFields(nil, sensitive, "key")
	if err != nil || out != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", out, err)
	}

	// Empty sensitive keys
	out, err = DecryptSensitiveFields(params, nil, "key")
	if err != nil || !reflect.DeepEqual(out, params) {
		t.Fatalf("expected (%v, nil), got (%v, %v)", params, out, err)
	}

	// Empty key with non-empty sensitive keys
	_, err = DecryptSensitiveFields(params, sensitive, "")
	if err == nil {
		t.Fatal("expected error for empty encryption key, got nil")
	}
}

func TestDecryptSensitiveFields_NonStringSensitiveField(t *testing.T) {
	params := map[string]any{"password": 12345}
	sensitive := map[string]bool{"password": true}

	_, err := DecryptSensitiveFields(params, sensitive, "key")
	if err == nil {
		t.Fatal("expected error when sensitive field is not a string, got nil")
	}
}

func TestDecryptSensitiveFields_InvalidCiphertext(t *testing.T) {
	params := map[string]any{"password": "invalid-ciphertext"}
	sensitive := map[string]bool{"password": true}

	_, err := DecryptSensitiveFields(params, sensitive, "key")
	if err == nil {
		t.Fatal("expected error when decrypting invalid ciphertext, got nil")
	}
}

func TestEncryptDecryptSensitiveFields_Roundtrip(t *testing.T) {
	key := "my-secret-key"
	params := map[string]any{
		"host":     "localhost",
		"port":     5432,
		"user":     "admin",
		"password": "super-secret-password",
		"token":    "bearer-token-1234",
	}
	sensitive := map[string]bool{
		"password": true,
		"token":    true,
	}

	encrypted, err := EncryptSensitiveFields(params, sensitive, key)
	if err != nil {
		t.Fatalf("EncryptSensitiveFields failed: %v", err)
	}

	// Non-sensitive fields unchanged
	if encrypted["host"] != "localhost" || encrypted["port"] != 5432 || encrypted["user"] != "admin" {
		t.Fatalf("non-sensitive fields modified: %v", encrypted)
	}

	// Sensitive fields encrypted (must not match plaintext)
	if encrypted["password"] == "super-secret-password" || encrypted["token"] == "bearer-token-1234" {
		t.Fatalf("sensitive fields were not encrypted: %v", encrypted)
	}

	decrypted, err := DecryptSensitiveFields(encrypted, sensitive, key)
	if err != nil {
		t.Fatalf("DecryptSensitiveFields failed: %v", err)
	}

	if !reflect.DeepEqual(decrypted, params) {
		t.Fatalf("roundtrip mismatch: want %v, got %v", params, decrypted)
	}
}

func TestStripSensitiveFields(t *testing.T) {
	metadata := map[string]any{
		"endpoint": "https://s3.amazonaws.com",
		"bucket":   "my-bucket",
		"secret":   "my-secret",
	}
	sensitive := map[string]bool{
		"secret": true,
	}

	stripped := StripSensitiveFields(metadata, sensitive)
	if stripped["secret"] != nil {
		t.Fatalf("expected secret to be stripped, got %v", stripped["secret"])
	}
	if stripped["endpoint"] != "https://s3.amazonaws.com" || stripped["bucket"] != "my-bucket" {
		t.Fatalf("non-sensitive fields missing: %v", stripped)
	}

	// Original map must not be mutated
	if metadata["secret"] != "my-secret" {
		t.Fatal("original metadata map was mutated")
	}

	// Nil metadata returns empty map
	nilResult := StripSensitiveFields(nil, sensitive)
	if len(nilResult) != 0 {
		t.Fatalf("expected empty map for nil metadata, got %v", nilResult)
	}

	// Nil sensitiveFields leaves all fields
	noSensitiveResult := StripSensitiveFields(metadata, nil)
	if len(noSensitiveResult) != len(metadata) {
		t.Fatalf("expected all fields preserved with nil sensitive map, got %v", noSensitiveResult)
	}
}

func TestSchemaFields(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"host": map[string]any{
				"type": "string",
			},
			"password": map[string]any{
				"type":       "string",
				"format":     "password",
				"ui:section": "Authentication",
			},
			"apiKey": map[string]any{
				"type":       "string",
				"format":     "password",
				"ui:section": "Authentication",
			},
		},
	}

	sensitive := SensitiveFieldsFromSchema(schema)
	if !sensitive["password"] || !sensitive["apiKey"] || sensitive["host"] {
		t.Fatalf("unexpected sensitive fields: %v", sensitive)
	}

	updatable := UpdatableFieldsFromSchema(schema)
	if !updatable["password"] || !updatable["apiKey"] || updatable["host"] {
		t.Fatalf("unexpected updatable fields: %v", updatable)
	}
}

func TestSchemaFields_EdgeCases(t *testing.T) {
	// Nil schema
	if len(SensitiveFieldsFromSchema(nil)) != 0 {
		t.Fatal("expected empty map for nil schema")
	}

	// Missing properties key
	if len(SensitiveFieldsFromSchema(map[string]any{"type": "object"})) != 0 {
		t.Fatal("expected empty map for schema without properties")
	}

	// Properties is not a map[string]any
	if len(SensitiveFieldsFromSchema(map[string]any{"properties": "not-a-map"})) != 0 {
		t.Fatal("expected empty map when properties is not a map")
	}

	// Invalid property element (not a map[string]any) or non-string attribute value
	schema := map[string]any{
		"properties": map[string]any{
			"invalidProp": "not-a-property-map",
			"nonStringVal": map[string]any{
				"format": 12345,
			},
			"validProp": map[string]any{
				"format": "password",
			},
		},
	}

	fields := SensitiveFieldsFromSchema(schema)
	if !fields["validProp"] || fields["invalidProp"] || fields["nonStringVal"] {
		t.Fatalf("unexpected result for edge case schema: %v", fields)
	}
}
