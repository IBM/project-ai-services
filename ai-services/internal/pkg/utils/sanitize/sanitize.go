package sanitize

import "regexp"

// Redacted is the placeholder written in place of a sensitive value.
const Redacted = "[REDACTED]"

// SecretSanitizer redacts values of sensitive map keys before they are
// serialised into log messages or error strings.
// Use NewSecretSanitizer to create an instance and SanitizeArgs as the entry point.
type SecretSanitizer struct {
	sensitiveKeyPatterns []*regexp.Regexp
}

// NewSecretSanitizer creates a SecretSanitizer with the default sensitive-key
// pattern set.
func NewSecretSanitizer() *SecretSanitizer {
	patterns := []*regexp.Regexp{
		// password / passwd
		regexp.MustCompile(`(?i).*passw(or)?d.*`),
		// secret
		regexp.MustCompile(`(?i).*secret.*`),
		// token — matches oauth_token, accessToken, refresh_token, X-Auth-Token.
		// "tokenizer" is intentionally also caught: map keys named "tokenizer"
		// do not appear in this codebase and erring on the side of redaction is safer.
		regexp.MustCompile(`(?i).*token.*`),
		// api key variants: apikey, api_key, api-key, ApiKey
		regexp.MustCompile(`(?i).*api.?key.*`),
		// access key variants: accesskey, access_key, accessKey
		regexp.MustCompile(`(?i).*access.?key.*`),
		// private key variants: privatekey, private_key, privateKey
		regexp.MustCompile(`(?i).*private.?key.*`),
		// credential / credentials
		regexp.MustCompile(`(?i).*credential.*`),
		// auth — matches auth, Authorization, x-auth, authToken, oauth_auth.
		// "author" / "authorName" are intentionally also caught: map keys named
		// "author" do not appear in this codebase and erring on redaction is safer.
		regexp.MustCompile(`(?i).*auth.*`),
		// certificate / cert / tls_cert
		regexp.MustCompile(`(?i).*cert.*`),
	}

	return &SecretSanitizer{sensitiveKeyPatterns: patterns}
}

// SanitizeArgs redacts sensitive values from any map arguments in args.
// Non-map arguments are passed through unchanged.
// If no map arguments are present the original slice is returned as-is (no allocation).
func (s *SecretSanitizer) SanitizeArgs(args []any) []any {
	hasMaps := false
	for _, a := range args {
		switch a.(type) {
		case map[string]any, map[string]string:
			hasMaps = true
		}

		if hasMaps {
			break
		}
	}

	if !hasMaps {
		return args
	}

	out := make([]any, len(args))
	for i, a := range args {
		out[i] = s.sanitizeArg(a)
	}

	return out
}

// isSensitiveKey reports whether the map key k should have its value redacted.
func (s *SecretSanitizer) isSensitiveKey(k string) bool {
	for _, re := range s.sensitiveKeyPatterns {
		if re.MatchString(k) {
			return true
		}
	}

	return false
}

// sanitizeMapAny returns a shallow copy of m with sensitive values redacted.
// Values that are themselves maps are sanitised recursively.
func (s *SecretSanitizer) sanitizeMapAny(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s.isSensitiveKey(k) {
			out[k] = Redacted

			continue
		}
		switch nested := v.(type) {
		case map[string]any:
			out[k] = s.sanitizeMapAny(nested)
		case map[string]string:
			out[k] = s.sanitizeMapString(nested)
		default:
			out[k] = v
		}
	}

	return out
}

// sanitizeMapString returns a shallow copy of m with sensitive values redacted.
func (s *SecretSanitizer) sanitizeMapString(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s.isSensitiveKey(k) {
			out[k] = Redacted
		} else {
			out[k] = v
		}
	}

	return out
}

// sanitizeArg returns a safe-to-log representation of a single argument.
//
//   - map[string]any    → copy with sensitive values replaced by Redacted
//   - map[string]string → copy with sensitive values replaced by Redacted
//   - anything else     → returned unchanged (no allocation, no reflection)
func (s *SecretSanitizer) sanitizeArg(arg any) any {
	switch v := arg.(type) {
	case map[string]any:
		return s.sanitizeMapAny(v)
	case map[string]string:
		return s.sanitizeMapString(v)
	default:
		return arg
	}
}
