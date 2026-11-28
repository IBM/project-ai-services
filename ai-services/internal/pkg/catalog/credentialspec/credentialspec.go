// Package credentialspec parses the @credential annotation of a service's values.yaml.
//
// A service that calls another service declares the credential it needs next to the values
// key that receives the name of the Secret holding it:
//
//	# @credential:service grants=similarity:read
//	authServiceCredentialSecret: ""
//
// The catalog provisions the credential at deploy time, stores it in a Secret and sets the
// key to that Secret's name. The credential value never passes through the values.
//
// The parser fails closed: anything it does not recognise is an error, never a silent skip.
package credentialspec

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Marker starts the annotation. Bundle validation refuses any values.yaml that contains it.
const Marker = "@credential:"

const (
	kindService = "service"
	grantsOpt   = "grants="
	grantParts  = 2
)

// Spec is one annotated values key.
type Spec struct {
	// ValuesKey is the top-level values key that receives the name of the Secret.
	ValuesKey string
	// Grants maps an audience (a service name) to the roles the credential may obtain.
	Grants map[string][]string
}

// Has reports whether data contains the annotation marker anywhere, including in places
// Parse would reject.
func Has(data []byte) bool {
	return bytes.Contains(data, []byte(Marker))
}

// allowedRoles is every role an annotation may grant. admin is deliberately absent.
var allowedRoles = map[string]bool{"read": true, "write": true}

// Parse returns the annotated keys of a values.yaml, in file order. It returns no specs for a
// file without the marker. An annotation that is malformed, sits anywhere but directly above
// a top-level key, or whose key is not an empty string is an error.
func Parse(data []byte) ([]Spec, error) {
	if !Has(data) {
		return nil, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse values.yaml: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("values.yaml has a @credential annotation but is not a mapping")
	}

	var specs []Spec
	doc := root.Content[0]
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, value := doc.Content[i], doc.Content[i+1]
		if !Has([]byte(key.HeadComment)) {
			continue
		}

		spec, err := parseKey(key, value)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	// Every marker must have produced a spec. A marker in prose, on a nested key, on the
	// first key of the file (where yaml attaches it to the document) or in a trailing
	// comment would otherwise be ignored without a word.
	if n := strings.Count(string(data), Marker); n != len(specs) {
		return nil, fmt.Errorf("found %d %s annotation(s) but only %d sit directly above a top-level key", n, strings.TrimSuffix(Marker, ":"), len(specs))
	}

	return specs, nil
}

func parseKey(key, value *yaml.Node) (Spec, error) {
	if value.Kind != yaml.ScalarNode || value.Value != "" {
		return Spec{}, fmt.Errorf("key %q: a key with a %s annotation must be an empty string", key.Value, strings.TrimSuffix(Marker, ":"))
	}

	line, err := annotationLine(key)
	if err != nil {
		return Spec{}, err
	}

	grants, err := parseAnnotation(line)
	if err != nil {
		return Spec{}, fmt.Errorf("key %q: %w", key.Value, err)
	}

	return Spec{ValuesKey: key.Value, Grants: grants}, nil
}

// annotationLine returns the single comment line that carries the marker.
func annotationLine(key *yaml.Node) (string, error) {
	var found string
	for line := range strings.SplitSeq(key.HeadComment, "\n") {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if !strings.HasPrefix(text, Marker) {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("key %q: more than one %s annotation", key.Value, strings.TrimSuffix(Marker, ":"))
		}
		found = text
	}
	if found == "" {
		return "", fmt.Errorf("key %q: %s must start its comment line", key.Value, strings.TrimSuffix(Marker, ":"))
	}

	return found, nil
}

// parseAnnotation parses "@credential:service grants=aud:role[,aud:role...]".
func parseAnnotation(line string) (map[string][]string, error) {
	kind, opts, _ := strings.Cut(strings.TrimPrefix(line, Marker), " ")
	if kind != kindService {
		return nil, fmt.Errorf("unsupported credential kind %q (only %q)", kind, kindService)
	}

	list, ok := strings.CutPrefix(strings.TrimSpace(opts), grantsOpt)
	if !ok {
		return nil, fmt.Errorf("expected %s<audience>:<role>[,<audience>:<role>...]", grantsOpt)
	}

	grants := make(map[string][]string)
	for entry := range strings.SplitSeq(list, ",") {
		aud, role, ok := splitGrant(entry)
		if !ok {
			return nil, fmt.Errorf("invalid grant %q (expected <audience>:<role>)", strings.TrimSpace(entry))
		}
		if !allowedRoles[role] {
			return nil, fmt.Errorf("grant %q: role %q is not allowed (read or write)", strings.TrimSpace(entry), role)
		}
		if !hasRole(grants[aud], role) {
			grants[aud] = append(grants[aud], role)
		}
	}

	return grants, nil
}

func splitGrant(entry string) (aud, role string, ok bool) {
	parts := strings.Split(strings.TrimSpace(entry), ":")
	if len(parts) != grantParts || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	return parts[0], parts[1], true
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}

	return false
}
