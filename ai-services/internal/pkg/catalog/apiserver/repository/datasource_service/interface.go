package datasourceservice

import (
	"context"
	"fmt"
	"strings"
)

// ConnectionCheckType identifies which phase of the connection test failed.
type ConnectionCheckType string

const (
	// ConnectionCheckNetwork indicates a TCP/DNS reachability failure.
	ConnectionCheckNetwork ConnectionCheckType = "network"
	// ConnectionCheckAuth indicates a credential rejection (invalid key, bad password, etc.).
	ConnectionCheckAuth ConnectionCheckType = "auth"
	// ConnectionCheckAccess indicates valid credentials but insufficient permissions or a missing resource.
	ConnectionCheckAccess ConnectionCheckType = "access"
)

// ConnectionCheckError is returned when one of the three sequential connection checks fails.
// It carries the check type and a human-readable message so callers can surface a specific,
// actionable error to the user.
type ConnectionCheckError struct {
	// CheckType is the phase of the test that failed (network, auth, or access).
	CheckType ConnectionCheckType
	// Message describes the failure in human-readable terms.
	Message string
}

func (e *ConnectionCheckError) Error() string {
	return fmt.Sprintf("[%s] %s", strings.ToUpper(string(e.CheckType)), e.Message)
}

// ConnectionTester is the interface implemented by each datasource provider.
// It captures only the behaviour that differs between providers; the generic
// CreateDatasource flow (validation, encryption, DB insert) lives in DatasourceService
// and delegates to this interface for the parts that vary.
type ConnectionTester interface {
	// TestConnection runs provider-specific connectivity checks (network → auth → access).
	// Returns nil when all checks pass, or a *ConnectionCheckError on the first failure.
	TestConnection(ctx context.Context, params map[string]any) error
}

// SensitiveFieldsFromSchema inspects the top-level properties of a JSON Schema map and
// returns the set of property names whose "format" is "password". This allows the set of
// fields that require encryption to be driven by the connector's schema.json rather than
// being hardcoded in each provider implementation.
// Exported so the ConnectorSyncJob can reuse it without duplicating the logic.
func SensitiveFieldsFromSchema(schema map[string]any) map[string]bool {
	return schemaFieldsBySection(schema, "format", "password")
}

// updatableFieldsFromSchema inspects the top-level properties of a JSON Schema map and
// returns the set of property names whose "ui:section" is "Authentication". These are the
// only fields that may be changed after a datasource is created; structural fields
// (Location, File filters, etc.) are immutable.
func updatableFieldsFromSchema(schema map[string]any) map[string]bool {
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

// Made with Bob
