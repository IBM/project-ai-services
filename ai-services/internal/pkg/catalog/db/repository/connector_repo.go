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

// ConnectorFilters defines optional filters and pagination parameters for List queries.
type ConnectorFilters struct {
	Status   models.ConnectorStatus // Optional: filter by connector status
	Provider string                 // Optional: filter by provider identifier (e.g. "s3", "ssh")
	Limit    int                    // Optional: maximum number of records to return
	Offset   int                    // Optional: number of records to skip
}

// ConnectorUpdateFields holds the credential metadata fields that may be changed after creation.
// Only the metadata JSONB column is mutable; name, type, and provider are immutable.
type ConnectorUpdateFields struct {
	Metadata map[string]any
}

// ConnectorRepository defines the interface for connector data operations.
type ConnectorRepository interface {
	// Insert creates a new connector, populating the ID, CreatedAt, and UpdatedAt fields on success.
	Insert(ctx context.Context, connector *models.Connector) error
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
	// GetCount returns the total count of connectors matching the filters.
	GetCount(ctx context.Context, filters *ConnectorFilters) (int, error)
	// Update applies a partial update of credential metadata fields only.
	// Name, type, and provider are immutable after creation.
	Update(ctx context.Context, id uuid.UUID, fields ConnectorUpdateFields) (*models.Connector, error)
	// Delete removes a connector from the database.
	// Callers are responsible for checking service_dependencies before invoking this method.
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
	"id", "name", "type", "provider", "status", "message", "created_by", "created_at", "updated_at",
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

// scanConnector scans a pgx.Rows row projected from nonSensitiveColumns into a Connector.
func scanConnector(rows pgx.Rows) (*models.Connector, error) {
	c := &models.Connector{}
	var message sql.NullString

	if err := rows.Scan(
		&c.ID, &c.Name, &c.Type, &c.Provider, &c.Status,
		&message, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan connector: %w", err)
	}

	if message.Valid {
		c.Message = message.String
	}

	return c, nil
}

// scanConnectorWithCreds scans a pgx.Rows row projected from allColumns into a Connector,
// populating the Metadata field. Must only be used on internal (non-response) paths.
func scanConnectorWithCreds(rows pgx.Rows) (*models.Connector, error) {
	c := &models.Connector{}
	var message sql.NullString
	var metadataJSON []byte

	// Column order must match allColumns: public cols first, then metadata.
	if err := rows.Scan(
		&c.ID, &c.Name, &c.Type, &c.Provider, &c.Status,
		&message, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &metadataJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to scan connector: %w", err)
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

// Insert creates a new connector row, populating the ID, CreatedAt, and UpdatedAt fields on success.
// Sensitive fields inside connector.Metadata are stored as-is for now.
// TODO: encrypt sensitive fields inside connector.Metadata before write.
func (r *connectorRepo) Insert(ctx context.Context, connector *models.Connector) error {
	if connector.ID == uuid.Nil {
		connector.ID = uuid.New()
	}

	metadataJSON, err := json.Marshal(connector.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal connector metadata: %w", err)
	}

	query := `
		INSERT INTO connectors (id, name, type, provider, status, message, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err = r.pool.QueryRow(
		ctx, query,
		connector.ID,
		connector.Name,
		connector.Type,
		connector.Provider,
		connector.Status,
		sql.NullString{String: connector.Message, Valid: connector.Message != ""},
		metadataJSON,
		connector.CreatedBy,
	).Scan(&connector.CreatedAt, &connector.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert connector: %w", err)
	}

	return nil
}

// GetByID retrieves a connector by UUID.
// Pass includeCreds=false for API responses (metadata omitted).
// Pass includeCreds=true for internal paths that need credentials (sync job, Digitize propagation);
// the caller must decrypt sensitive fields in-memory and must never forward the value to a response.
// TODO: decrypt sensitive fields in Metadata when includeCreds is true.
func (r *connectorRepo) GetByID(ctx context.Context, id uuid.UUID, includeCreds bool) (*models.Connector, error) {
	var colList string
	if includeCreds {
		colList = allColumns
	} else {
		colList = nonSensitiveColumns
	}

	query := `SELECT ` + colList + ` FROM connectors WHERE id = $1`

	rows, err := r.pool.Query(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get connector: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("failed to get connector: %w", err)
		}

		return nil, ErrConnectorNotFound
	}

	if includeCreds {
		return scanConnectorWithCreds(rows)
	}

	return scanConnector(rows)
}

// GetCount returns the total count of connectors matching the filters.
func (r *connectorRepo) GetCount(ctx context.Context, filters *ConnectorFilters) (int, error) {
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

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM connectors`+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("failed to count connectors: %w", err)
	}

	return total, nil
}

// buildListQuery constructs the SELECT query and arguments for List.
func (r *connectorRepo) buildListQuery(filters *ConnectorFilters) (string, []interface{}) {
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

	query := `SELECT ` + nonSensitiveColumns + ` FROM connectors` + where + ` ORDER BY created_at DESC`

	if filters != nil {
		if filters.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
			args = append(args, filters.Limit)
		}
		if filters.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
			args = append(args, filters.Offset)
		}
	}

	return query, args
}

// List returns a page of connectors matching optional filters and the total count of matching rows.
// Sensitive metadata is not selected.
func (r *connectorRepo) List(ctx context.Context, filters *ConnectorFilters) ([]models.Connector, int, error) {
	total, err := r.GetCount(ctx, filters)
	if err != nil {
		return nil, 0, err
	}

	listQuery, args := r.buildListQuery(filters)

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list connectors: %w", err)
	}
	defer rows.Close()

	var connectors []models.Connector
	for rows.Next() {
		c, err := scanConnector(rows)
		if err != nil {
			return nil, 0, err
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

	rows, err := r.pool.Query(ctx, query, metadataJSON, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update connector: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("failed to update connector: %w", err)
		}

		return nil, ErrConnectorNotFound
	}

	return scanConnector(rows)
}

// Delete removes a connector from the database.
// Callers must check service_dependencies (via ServiceDependencyRepository.GetServicesByDependency)
// and enforce any in-use guard before calling this method.
func (r *connectorRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM connectors WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
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
