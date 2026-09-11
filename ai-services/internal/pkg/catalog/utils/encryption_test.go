package utils

import (
	"encoding/base64"
	"strings"
	"testing"
)

// ── Encrypt ───────────────────────────────────────────────────────────────────

func TestEncrypt_EmptySecret(t *testing.T) {
	_, err := Encrypt("plaintext", "")
	if err == nil {
		t.Fatal("expected error for empty secret, got nil")
	}
}

func TestEncrypt_ProducesBase64(t *testing.T) {
	out, err := Encrypt("hello", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, decErr := base64.StdEncoding.DecodeString(out); decErr != nil {
		t.Fatalf("Encrypt output is not valid base64: %v", decErr)
	}
}

func TestEncrypt_OutputDoesNotContainPlaintext(t *testing.T) {
	plaintext := "super-secret-value"
	out, err := Encrypt(plaintext, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, plaintext) {
		t.Fatal("ciphertext must not contain the plaintext verbatim")
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	// Two encryptions of the same plaintext with the same key must differ
	// because a fresh random nonce is generated each time.
	a, err := Encrypt("same", "key")
	if err != nil {
		t.Fatalf("first Encrypt: %v", err)
	}
	b, err := Encrypt("same", "key")
	if err != nil {
		t.Fatalf("second Encrypt: %v", err)
	}
	if a == b {
		t.Fatal("two Encrypt calls with the same input must produce different ciphertext (random nonce)")
	}
}

// ── Decrypt ───────────────────────────────────────────────────────────────────

func TestDecrypt_EmptySecret(t *testing.T) {
	blob, _ := Encrypt("x", "key")
	_, err := Decrypt(blob, "")
	if err == nil {
		t.Fatal("expected error for empty secret, got nil")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	_, err := Decrypt("not-valid-base64!!!", "key")
	if err == nil {
		t.Fatal("expected error for non-base64 input, got nil")
	}
}

func TestDecrypt_TruncatedBlob(t *testing.T) {
	// A valid base64 string that decodes to fewer bytes than a GCM nonce (12).
	short := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	_, err := Decrypt(short, "key")
	if err == nil {
		t.Fatal("expected error for truncated ciphertext, got nil")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	blob, err := Encrypt("secret", "correct-key")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = Decrypt(blob, "wrong-key")
	if err == nil {
		t.Fatal("expected authentication error when decrypting with the wrong key")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	blob, err := Encrypt("secret", "key")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(blob)
	// Flip the last byte to break the GCM authentication tag.
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)

	_, err = Decrypt(tampered, "key")
	if err == nil {
		t.Fatal("expected authentication error for tampered ciphertext")
	}
}

// ── Roundtrip ─────────────────────────────────────────────────────────────────

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	cases := []struct {
		name      string
		plaintext string
		secret    string
	}{
		{"simple", "hello world", "my-secret"},
		{"empty plaintext", "", "key"},
		{"long plaintext", strings.Repeat("a", 4096), "key"},
		{"unicode", "日本語テスト", "unicode-key"},
		{"pem block", "-----BEGIN EC PRIVATE KEY-----\nfake\n-----END EC PRIVATE KEY-----\n", "pem-key"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := Encrypt(tc.plaintext, tc.secret)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			got, err := Decrypt(blob, tc.secret)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if got != tc.plaintext {
				t.Fatalf("roundtrip mismatch: want %q, got %q", tc.plaintext, got)
			}
		})
	}
}

func TestEncryptDecrypt_KeyDeterminism(t *testing.T) {
	// The same secret must always derive the same AES key, so a blob encrypted
	// in one call must be decryptable in a completely independent call.
	const secret = "deterministic-secret"
	blob, err := Encrypt("payload", secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(blob, secret)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "payload" {
		t.Fatalf("expected %q, got %q", "payload", got)
	}
}
