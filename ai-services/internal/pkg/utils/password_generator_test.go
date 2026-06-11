package utils

import (
	"strings"
	"testing"
	"unicode"
)

func TestGenerateRandomPassword(t *testing.T) {
	password, err := GenerateRandomPassword()
	if err != nil {
		t.Fatalf("GenerateRandomPassword() failed: %v", err)
	}

	if len(password) != DefaultPasswordLength {
		t.Errorf("Expected password length %d, got %d", DefaultPasswordLength, len(password))
	}

	verifyPasswordRequirements(t, password, passwordOptions{
		Lower:   true,
		Upper:   true,
		Digits:  true,
		Special: true,
	})
}

func TestGenerateRandomPasswordWithOptions(t *testing.T) {
	tests := []struct {
		name string
		opts passwordOptions
		want passwordOptions // expected character requirements
		err  bool
	}{
		{
			name: "all character types enabled",
			opts: passwordOptions{Length: 20, Lower: true, Upper: true, Digits: true, Special: true},
			want: passwordOptions{Lower: true, Upper: true, Digits: true, Special: true},
			err:  false,
		},
		{
			name: "only lowercase",
			opts: passwordOptions{Length: 16, Lower: true, Upper: false, Digits: false, Special: false},
			want: passwordOptions{Lower: true, Upper: false, Digits: false, Special: false},
			err:  false,
		},
		{
			name: "only uppercase",
			opts: passwordOptions{Length: 16, Lower: false, Upper: true, Digits: false, Special: false},
			want: passwordOptions{Lower: false, Upper: true, Digits: false, Special: false},
			err:  false,
		},
		{
			name: "only digits",
			opts: passwordOptions{Length: 16, Lower: false, Upper: false, Digits: true, Special: false},
			want: passwordOptions{Lower: false, Upper: false, Digits: true, Special: false},
			err:  false,
		},
		{
			name: "only special characters",
			opts: passwordOptions{Length: 16, Lower: false, Upper: false, Digits: false, Special: true},
			want: passwordOptions{Lower: false, Upper: false, Digits: false, Special: true},
			err:  false,
		},
		{
			name: "lowercase and uppercase",
			opts: passwordOptions{Length: 16, Lower: true, Upper: true, Digits: false, Special: false},
			want: passwordOptions{Lower: true, Upper: true, Digits: false, Special: false},
			err:  false,
		},
		{
			name: "digits and special",
			opts: passwordOptions{Length: 16, Lower: false, Upper: false, Digits: true, Special: true},
			want: passwordOptions{Lower: false, Upper: false, Digits: true, Special: true},
			err:  false,
		},
		{
			name: "minimum length with all types",
			opts: passwordOptions{Length: 4, Lower: true, Upper: true, Digits: true, Special: true},
			want: passwordOptions{Lower: true, Upper: true, Digits: true, Special: true},
			err:  false,
		},
		{
			name: "zero length",
			opts: passwordOptions{Length: 0, Lower: true},
			err:  true,
		},
		{
			name: "negative length",
			opts: passwordOptions{Length: -1, Lower: true},
			err:  true,
		},
		{
			name: "length less than required types",
			opts: passwordOptions{Length: 2, Lower: true, Upper: true, Digits: true, Special: true},
			err:  true,
		},
		{
			name: "no character types enabled",
			opts: passwordOptions{Length: 16, Lower: false, Upper: false, Digits: false, Special: false},
			err:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			password, err := generateRandomPasswordWithOptions(tt.opts)

			if tt.err {
				if err == nil {
					t.Error("Expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(password) != tt.opts.Length {
				t.Errorf("Expected password length %d, got %d", tt.opts.Length, len(password))
			}

			verifyPasswordRequirements(t, password, tt.want)

			// Verify first character is not special if special chars are enabled AND other types exist
			hasNonSpecial := tt.opts.Lower || tt.opts.Upper || tt.opts.Digits
			if tt.opts.Special && hasNonSpecial && isSpecialChar(password[0]) {
				t.Error("First character should not be a special character when non-special types are available")
			}
		})
	}
}

func TestGenerateRandomPasswordWithOptions_Uniqueness(t *testing.T) {
	opts := passwordOptions{
		Length:  16,
		Lower:   true,
		Upper:   true,
		Digits:  true,
		Special: true,
	}

	passwords := make(map[string]bool)
	iterations := 100

	for range iterations {
		password, err := generateRandomPasswordWithOptions(opts)
		if err != nil {
			t.Fatalf("generateRandomPasswordWithOptions() failed: %v", err)
		}
		passwords[password] = true
	}

	// Expect at least 95% uniqueness
	minUnique := iterations * 95 / 100
	if len(passwords) < minUnique {
		t.Errorf("Expected at least %d unique passwords, got %d", minUnique, len(passwords))
	}
}

func TestParsePasswordOptions(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		want       passwordOptions
		err        bool
	}{
		{
			name:       "default options",
			annotation: "@generate:password",
			want:       passwordOptions{Length: DefaultPasswordLength, Lower: true, Upper: true, Digits: true, Special: true},
			err:        false,
		},
		{
			name:       "custom length",
			annotation: "@generate:password length=24",
			want:       passwordOptions{Length: 24, Lower: true, Upper: true, Digits: true, Special: true},
			err:        false,
		},
		{
			name:       "disable special and upper",
			annotation: "@generate:password special=false, upper=false",
			want:       passwordOptions{Length: DefaultPasswordLength, Lower: true, Upper: false, Digits: true, Special: false},
			err:        false,
		},
		{
			name:       "complex annotation",
			annotation: "@generate:password length=32, lower=true, upper=true, digits=false, special=false",
			want:       passwordOptions{Length: 32, Lower: true, Upper: true, Digits: false, Special: false},
			err:        false,
		},
		{
			name:       "all options specified",
			annotation: "@generate:password length=20, lower=false, upper=true, digits=true, special=false",
			want:       passwordOptions{Length: 20, Lower: false, Upper: true, Digits: true, Special: false},
			err:        false,
		},
		{
			name:       "missing password type",
			annotation: "@generate:",
			err:        true,
		},
		{
			name:       "wrong type",
			annotation: "@generate:token",
			err:        true,
		},
		{
			name:       "malformed annotation",
			annotation: "generate:password",
			err:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parsePasswordOptions(tt.annotation)

			if tt.err {
				if err == nil {
					t.Error("Expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if opts.Length != tt.want.Length {
				t.Errorf("Length: expected %d, got %d", tt.want.Length, opts.Length)
			}
			if opts.Lower != tt.want.Lower {
				t.Errorf("Lower: expected %v, got %v", tt.want.Lower, opts.Lower)
			}
			if opts.Upper != tt.want.Upper {
				t.Errorf("Upper: expected %v, got %v", tt.want.Upper, opts.Upper)
			}
			if opts.Digits != tt.want.Digits {
				t.Errorf("Digits: expected %v, got %v", tt.want.Digits, opts.Digits)
			}
			if opts.Special != tt.want.Special {
				t.Errorf("Special: expected %v, got %v", tt.want.Special, opts.Special)
			}
		})
	}
}

func TestBuildRequiredCharsets(t *testing.T) {
	tests := []struct {
		name     string
		opts     passwordOptions
		expected int
	}{
		{
			name:     "all enabled",
			opts:     passwordOptions{Lower: true, Upper: true, Digits: true, Special: true},
			expected: 4,
		},
		{
			name:     "only lowercase",
			opts:     passwordOptions{Lower: true},
			expected: 1,
		},
		{
			name:     "lowercase and digits",
			opts:     passwordOptions{Lower: true, Digits: true},
			expected: 2,
		},
		{
			name:     "upper and special",
			opts:     passwordOptions{Upper: true, Special: true},
			expected: 2,
		},
		{
			name:     "none enabled",
			opts:     passwordOptions{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sets := buildRequiredCharsets(tt.opts)
			if len(sets) != tt.expected {
				t.Errorf("Expected %d character sets, got %d", tt.expected, len(sets))
			}
		})
	}
}

func TestShuffleBytes(t *testing.T) {
	original := []byte("abcdefghijklmnopqrstuvwxyz")
	data := make([]byte, len(original))
	copy(data, original)

	err := shuffleBytes(data)
	if err != nil {
		t.Fatalf("shuffleBytes() failed: %v", err)
	}

	// Verify length is preserved
	if len(data) != len(original) {
		t.Errorf("Expected length %d, got %d", len(original), len(data))
	}

	// Verify all characters are still present
	originalMap := make(map[byte]int)
	shuffledMap := make(map[byte]int)

	for _, b := range original {
		originalMap[b]++
	}
	for _, b := range data {
		shuffledMap[b]++
	}

	for k, v := range originalMap {
		if shuffledMap[k] != v {
			t.Errorf("Character %c count mismatch: expected %d, got %d", k, v, shuffledMap[k])
		}
	}
}

func TestIsSpecialChar(t *testing.T) {
	tests := []struct {
		char     byte
		expected bool
	}{
		{'@', true},
		{'#', true},
		{'$', true},
		{'%', true},
		{'^', true},
		{'*', true},
		{'-', true},
		{'_', true},
		{'+', true},
		{'a', false},
		{'Z', false},
		{'5', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			result := isSpecialChar(tt.char)
			if result != tt.expected {
				t.Errorf("isSpecialChar(%c) = %v, expected %v", tt.char, result, tt.expected)
			}
		})
	}
}

// Helper functions

func verifyPasswordRequirements(t *testing.T, password string, opts passwordOptions) {
	t.Helper()

	hasLower := containsLowercase(password)
	hasUpper := containsUppercase(password)
	hasDigit := containsDigit(password)
	hasSpecial := containsSpecial(password)

	if opts.Lower && !hasLower {
		t.Error("Password should contain at least one lowercase character")
	}
	if !opts.Lower && hasLower {
		t.Error("Password should not contain lowercase characters")
	}

	if opts.Upper && !hasUpper {
		t.Error("Password should contain at least one uppercase character")
	}
	if !opts.Upper && hasUpper {
		t.Error("Password should not contain uppercase characters")
	}

	if opts.Digits && !hasDigit {
		t.Error("Password should contain at least one digit")
	}
	if !opts.Digits && hasDigit {
		t.Error("Password should not contain digits")
	}

	if opts.Special && !hasSpecial {
		t.Error("Password should contain at least one special character")
	}
	if !opts.Special && hasSpecial {
		t.Error("Password should not contain special characters")
	}
}

func containsLowercase(s string) bool {
	for _, r := range s {
		if unicode.IsLower(r) {
			return true
		}
	}

	return false
}

func containsUppercase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func containsDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsSpecial(s string) bool {
	for _, r := range s {
		if strings.ContainsRune(specialChars, r) {
			return true
		}
	}
	return false
}

// Made with Bob
