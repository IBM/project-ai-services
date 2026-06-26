package sanitize

import "regexp"

// Redacted is the placeholder written in place of a sensitive value.
const Redacted = "[REDACTED]"

// SecretSanitizer redacts sensitive values from map arguments before they are
// serialised (e.g. into log messages or error strings).
//
// A map key is considered sensitive when it matches any of the compiled
// patterns.  Each pattern uses (?i) for case-insensitive matching and .*
// anchors so partial key names are caught (e.g. "dbPassword", "tls_cert").
//
// False-positive mitigation:
//   - bare "key"     → catches "componentKey", "cacheKey" — replaced by the
//     more specific api.*key / private.*key / access.*key.
//   - "token"        → catches "tokenizer" — scoped with \btoken\b.
//   - "auth"         → catches "author"    — scoped with \bauth\b.
//   - bare "private" → catches "isPrivate" — scoped to private.*key.
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

// SanitizeArgs is the single public entry point used by the logger package.
// It returns a new slice with every sensitive map argument sanitised.
// Returns the original slice unchanged when no map argument is present,
// avoiding any allocation on the hot path.
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
