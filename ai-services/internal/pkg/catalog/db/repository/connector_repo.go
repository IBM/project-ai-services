package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ErrConnectorNotFound is returned when a connector cannot be located by its ID.
var ErrConnectorNotFound = errors.New("connector not found")

// ErrConnectorInUse is returned when a connector has active service_dependencies links
// and therefore cannot be deleted (409 Conflict).
var ErrConnectorInUse = errors.New("connector is linked to one or more services and cannot be deleted")

// ConnectorFilters defines optional filters and pagination parameters for List queries.
type ConnectorFilters struct {
	Status   string // Optional: filter by connector status ("Connected" or "Offline")
	Provider string // Optional: filter by provider identifier (e.g. "s3", "ssh")
	Limit    int    // Optional: maximum number of records to return
	Offset   int    // Optional: number of records to skip
}

// ConnectorUpdateFields holds the credential metadata fields that may be changed after creation.
// Only the metadata JSONB column is mutable; name, type, and provider are immutable.
type ConnectorUpdateFields struct {
	Metadata map[string]any
}

// ConnectorRepository defines the interface for connector data operations.
type ConnectorRepository interface {
	// Create inserts a new connector and returns the persisted record.
	Create(ctx context.Context, connector *models.Connector) (*models.Connector, error)
	// GetByID retrieves a connector by its UUID.
	// When includeCreds is false the metadata column is omitted (safe for API responses).
	// When includeCreds is true the full row including metadata is returned; callers are
	// responsible for decrypting sensitive fields in-memory and must never forward the
	// raw value to any API response.
	// Returns ErrConnectorNotFound if the row does not exist.
	// TODO: decrypt sensitive fields in Metadata when includeCreds is true.
	GetByID(ctx context.Context, id uuid.UUID, includeCreds bool) (*models.Connector, error)
	// List returns a page of connectors matching the optional filters together with the
	// total count of matching rows (for pagination metadata).
	// Sensitive metadata is never included in the returned structs.
	List(ctx context.Context, filters *ConnectorFilters) ([]models.Connector, int, error)
	// Update applies a partial update of credential metadata fields only.
	// Name, type, and provider are immutable after creation.
	Update(ctx context.Context, id uuid.UUID, fields ConnectorUpdateFields) (*models.Connector, error)
	// Delete removes a connector; returns ErrConnectorInUse if active service_dependencies rows exist.
	Delete(ctx context.Context, id uuid.UUID) error
	// UpdateStatus sets the status and message columns for the given connector.
	// Used by the ConnectorSyncJob heartbeat.
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.ConnectorStatus, message string) error
}

// connectorRepo implements ConnectorRepository using pgx.
type connectorRepo struct {
	pool *pgxpool.Pool
}

// NewConnectorRepository creates a new ConnectorRepository backed by the provided connection pool.
func NewConnectorRepository(pool *pgxpool.Pool) ConnectorRepository {
	return &connectorRepo{pool: pool}
}

// connectorPublicCols are the columns that are safe to return in API responses.
var connectorPublicCols = []string{
	"id", "name", "type", "provider", "status", "message", "created_at", "updated_at",
}

// connectorSensitiveCols are the credential columns that must never appear in API responses.
// They are appended to connectorPublicCols to form the full column list for internal queries.
var connectorSensitiveCols = []string{
	"metadata",
}

// nonSensitiveColumns is the SELECT projection for API-safe queries.
var nonSensitiveColumns = strings.Join(connectorPublicCols, ", ")

// allColumns appends the sensitive columns to the public ones for internal-use queries
// (sync job, Digitize propagation). Never use this on any response path.
var allColumns = strings.Join(append(connectorPublicCols, connectorSensitiveCols...), ", ")

// scanConnector scans a row projected from nonSensitiveColumns into a Connector.
func scanConnector(row interface {
	Scan(dest ...any) error
}) (*models.Connector, error) {
	c := &models.Connector{}
	var message sql.NullString

	if err := row.Scan(
		&c.ID, &c.Name, &c.Type, &c.Provider, &c.Status,
		&message, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if message.Valid {
		c.Message = message.String
	}

	return c, nil
}

// scanConnectorWithCreds scans a row projected from allColumns into a Connector,
// populating the Metadata field. Must only be used on internal (non-response) paths.
func scanConnectorWithCreds(row interface {
	Scan(dest ...any) error
}) (*models.Connector, error) {
	c := &models.Connector{}
	var message sql.NullString
	var metadataJSON []byte

	if err := row.Scan(
		&c.ID, &c.Name, &c.Type, &c.Provider, &c.Status,
		&message, &metadataJSON, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if message.Valid {
		c.Message = message.String
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &c.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal connector metadata: %w", err)
		}
	}

	return c, nil
}

// Create inserts a new connector row and returns the persisted record.
// Sensitive fields inside connector.Metadata are stored as-is for now.
// TODO: encrypt sensitive fields inside connector.Metadata before write.
func (r *connectorRepo) Create(ctx context.Context, connector *models.Connector) (*models.Connector, error) {
	if connector.ID == uuid.Nil {
		connector.ID = uuid.New()
	}

	metadataJSON, err := json.Marshal(connector.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connector metadata: %w", err)
	}

	query := `
		INSERT INTO connectors (id, name, type, provider, status, message, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + nonSensitiveColumns

	row := r.pool.QueryRow(
		ctx, query,
		connector.ID,
		connector.Name,
		connector.Type,
		connector.Provider,
		connector.Status,
		sql.NullString{String: connector.Message, Valid: connector.Message != ""},
		metadataJSON,
	)

	created, err := scanConnector(row)
	if err != nil {
		return nil, fmt.Errorf("failed to insert connector: %w", err)
	}

	return created, nil
}

// GetByID retrieves a connector by UUID.
// Pass includeCreds=false for API responses (metadata omitted).
// Pass includeCreds=true for internal paths that need credentials (sync job, Digitize propagation);
// the caller must decrypt sensitive fields in-memory and must never forward the value to a response.
// TODO: decrypt sensitive fields in Metadata when includeCreds is true.
func (r *connectorRepo) GetByID(ctx context.Context, id uuid.UUID, includeCreds bool) (*models.Connector, error) {
	var (
		connector *models.Connector
		err       error
	)

	if includeCreds {
		query := `SELECT ` + allColumns + ` FROM connectors WHERE id = $1`
		connector, err = scanConnectorWithCreds(r.pool.QueryRow(ctx, query, id))
	} else {
		query := `SELECT ` + nonSensitiveColumns + ` FROM connectors WHERE id = $1`
		connector, err = scanConnector(r.pool.QueryRow(ctx, query, id))
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectorNotFound
		}
		return nil, fmt.Errorf("failed to get connector: %w", err)
	}

	return connector, nil
}

// List returns a page of connectors matching optional filters and the total count of matching rows.
// Sensitive metadata is not selected.
func (r *connectorRepo) List(ctx context.Context, filters *ConnectorFilters) ([]models.Connector, int, error) {
	args := []interface{}{}
	whereClauses := []string{}

	if filters != nil {
		if filters.Status != "" {
			whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", len(args)+1))
			args = append(args, filters.Status)
		}
		if filters.Provider != "" {
			whereClauses = append(whereClauses, fmt.Sprintf("provider = $%d", len(args)+1))
			args = append(args, filters.Provider)
		}
	}

	where := ""
	if len(whereClauses) > 0 {
		where = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Capture the arg slice length before appending pagination args so the count
	// query uses the same set of filter arguments.
	filterArgs := make([]interface{}, len(args))
	copy(filterArgs, args)

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM connectors`+where, filterArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count connectors: %w", err)
	}

	listQuery := `SELECT ` + nonSensitiveColumns + ` FROM connectors` + where + ` ORDER BY created_at DESC`

	if filters != nil {
		if filters.Limit > 0 {
			listQuery += fmt.Sprintf(" LIMIT $%d", len(args)+1)
			args = append(args, filters.Limit)
		}
		if filters.Offset > 0 {
			listQuery += fmt.Sprintf(" OFFSET $%d", len(args)+1)
			args = append(args, filters.Offset)
		}
	}

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list connectors: %w", err)
	}
	defer rows.Close()

	var connectors []models.Connector
	for rows.Next() {
		c, err := scanConnector(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan connector: %w", err)
		}
		connectors = append(connectors, *c)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating connectors: %w", err)
	}

	return connectors, total, nil
}

// Update replaces the metadata JSONB column for the given connector.
// Name, type, and provider are immutable and are never touched here.
// Sensitive fields inside fields.Metadata are stored as-is for now.
// TODO: encrypt sensitive fields inside fields.Metadata before write.
func (r *connectorRepo) Update(ctx context.Context, id uuid.UUID, fields ConnectorUpdateFields) (*models.Connector, error) {
	metadataJSON, err := json.Marshal(fields.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connector metadata: %w", err)
	}

	query := `
		UPDATE connectors
		SET metadata = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING ` + nonSensitiveColumns

	updated, err := scanConnector(r.pool.QueryRow(ctx, query, metadataJSON, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectorNotFound
		}
		return nil, fmt.Errorf("failed to update connector: %w", err)
	}

	return updated, nil
}

// Delete removes a connector. Returns ErrConnectorInUse (409) if any service_dependencies
// rows reference this connector with dependency_type = 'datasource'.
func (r *connectorRepo) Delete(ctx context.Context, id uuid.UUID) error {
	var linkCount int
	checkQuery := `
		SELECT COUNT(*)
		FROM service_dependencies
		WHERE dependency_id = $1 AND dependency_type = 'datasource'
	`
	if err := r.pool.QueryRow(ctx, checkQuery, id).Scan(&linkCount); err != nil {
		return fmt.Errorf("failed to check connector links: %w", err)
	}
	if linkCount > 0 {
		return ErrConnectorInUse
	}

	_, err := r.pool.Exec(ctx, `DELETE FROM connectors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete connector: %w", err)
	}

	return nil
}

// UpdateStatus sets the status and message columns for the given connector.
// Used by the ConnectorSyncJob to record the outcome of each heartbeat cycle.
func (r *connectorRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status models.ConnectorStatus, message string) error {
	query := `
		UPDATE connectors
		SET status = $1, message = $2, updated_at = NOW()
		WHERE id = $3
	`

	_, err := r.pool.Exec(ctx, query, status, sql.NullString{String: message, Valid: message != ""}, id)
	if err != nil {
		return fmt.Errorf("failed to update connector status: %w", err)
	}

	return nil
}
