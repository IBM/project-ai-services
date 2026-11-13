package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
)

// modelSearchColumns defines the columns searched when a ?q= term is provided.
// Add column names here to extend search to additional fields in future.
var modelSearchColumns = []string{"name"}

// ModelFilters defines the optional filters and the pagination of the model list.
// Every empty field is not added to the WHERE clause.
// The values of one multi-value filter (Providers, Models) are OR'd; different filters are AND'd.
type ModelFilters struct {
	CreatedBy      string                   // Optional: filter by created_by (owner)
	DeploymentType constants.DeploymentType // Optional: "local" (components) or "remote" (connectors)
	ComponentType  string                   // Optional: model role, e.g. "llm"
	Providers      []string                 // Optional: provider ids, e.g. "vllm-spyre"; a model matches any of them
	Models         []string                 // Optional: model identifiers; a model matches any of them
	Status         string                   // Optional: component or connector status
	Search         string                   // Optional: case-insensitive substring search on the model name
	Limit          int                      // Optional: maximum number of records to return
	Offset         int                      // Optional: number of records to skip
}

// ModelRow is one model of the unified local + remote list.
// Remote rows have no WorkerID, WorkerName or WorkerRuntimeType.
type ModelRow struct {
	ID                uuid.UUID
	DeploymentType    constants.DeploymentType
	Name              string
	ComponentType     string
	Provider          string
	Model             string
	Params            map[string]any
	Status            string
	Message           string
	WorkerID          string
	WorkerName        string
	WorkerRuntimeType string
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ModelRepository reads the unified model list across the components and connectors tables.
type ModelRepository interface {
	// List returns one page of models matching the filters.
	List(ctx context.Context, filters *ModelFilters, componentTypes []string) ([]ModelRow, error)
	// GetCount returns the total number of models matching the filters (ignoring Limit and Offset).
	GetCount(ctx context.Context, filters *ModelFilters, componentTypes []string) (int, error)
	// GetByID returns the local or remote model with the given id, whoever created it: the caller
	// compares ModelRow.CreatedBy with the requesting user. Returns nil, nil when there is no such model.
	GetByID(ctx context.Context, id uuid.UUID, componentTypes []string) (*ModelRow, error)
}

// modelRepo implements ModelRepository using pgx.
type modelRepo struct {
	pool *pgxpool.Pool
}

// NewModelRepository creates a new ModelRepository backed by the provided connection pool.
func NewModelRepository(pool *pgxpool.Pool) ModelRepository {
	return &modelRepo{pool: pool}
}

// modelUnionQuery projects local models (components) and remote models (connectors) into one
// shape. The branches are UNION ALL'd: an id lives in exactly one table, so there is nothing to
// de-duplicate, and filters, ordering and paging are applied once over the whole set.
//
//   - Local rows are the components. Which of them are models is decided by the caller, from the
//     catalog: the componentTypes argument keeps only the component types whose providers declare
//     `category: models`.
//   - Remote rows are the connectors of type "models", which keeps datasource connectors out.
//   - The model and params are read from the metadata keys the planner persists.
//
// The values inlined here are trusted constants, never user input.
var modelUnionQuery = fmt.Sprintf(`
	SELECT c.id,
	       '%[1]s'                                     AS deployment_type,
	       c.name                                      AS name,
	       COALESCE(c.type, '')                        AS component_type,
	       COALESCE(c.provider, '')                    AS provider,
	       COALESCE(c.metadata->>'%[3]s', '')          AS model,
	       c.metadata->'%[4]s'                         AS params,
	       COALESCE(c.status::text, '')                AS status,
	       c.message                                   AS message,
	       w.id::text                                  AS worker_id,
	       w.name                                      AS worker_name,
	       w.runtime_type::text                        AS worker_runtime_type,
	       c.created_by                                AS created_by,
	       c.created_at                                AS created_at,
	       c.updated_at                                AS updated_at
	FROM components c
	LEFT JOIN workers w ON w.id = c.worker_id
	UNION ALL
	SELECT cn.id,
	       '%[2]s',
	       cn.name,
	       COALESCE(cn.metadata->>'%[5]s', ''),
	       cn.provider,
	       COALESCE(cn.metadata->>'%[3]s', ''),
	       cn.metadata->'%[4]s',
	       cn.status::text,
	       cn.message,
	       NULL::text,
	       NULL::text,
	       NULL::text,
	       cn.created_by,
	       cn.created_at,
	       cn.updated_at
	FROM connectors cn
	WHERE cn.type = '%[6]s'`,
	constants.DeploymentTypeLocal,
	constants.DeploymentTypeRemote,
	constants.MetadataKeyModel,
	constants.MetadataKeyParams,
	constants.MetadataKeyComponentType,
	constants.ConnectorTypeModels,
)

const modelColumns = "id, deployment_type, name, component_type, provider, model, params, " +
	"status, message, worker_id, worker_name, worker_runtime_type, created_by, created_at, updated_at"

// buildModelWhere builds the WHERE clause applied over the union and its positional arguments.
// The owner clause, when there is an owner, comes first, then the model component types.
func buildModelWhere(f *ModelFilters, componentTypes []string) (string, []interface{}) {
	var (
		args    []interface{}
		clauses []string
	)

	add := func(column, value string) {
		if value == "" {
			return
		}

		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}

	addAny := func(column string, values []string) {
		if len(values) == 0 {
			return
		}

		args = append(args, values)
		clauses = append(clauses, fmt.Sprintf("%s = ANY($%d)", column, len(args)))
	}

	add("created_by", f.CreatedBy)

	// An empty, non-nil list matches nothing: with no model types there are no models.
	if componentTypes != nil {
		args = append(args, componentTypes)
		clauses = append(clauses, fmt.Sprintf("component_type = ANY($%d)", len(args)))
	}

	add("deployment_type", string(f.DeploymentType))
	add("component_type", f.ComponentType)
	addAny("provider", f.Providers)
	addAny("model", f.Models)
	add("status", f.Status)

	if f.Search != "" {
		clauses = append(clauses, buildSearchClause(modelSearchColumns, &args, f.Search))
	}

	if len(clauses) == 0 {
		return "", nil
	}

	return " WHERE " + strings.Join(clauses, " AND "), args
}

// buildModelListQuery builds the paged SELECT over the union. Newest first, with the id as a
// tie-breaker so that pages are stable.
func buildModelListQuery(f *ModelFilters, componentTypes []string) (string, []interface{}) {
	where, args := buildModelWhere(f, componentTypes)

	query := "SELECT " + modelColumns + " FROM (" + modelUnionQuery + ") m" + where +
		" ORDER BY created_at DESC, id ASC"

	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, f.Limit)
	}

	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, f.Offset)
	}

	return query, args
}

// buildModelCountQuery builds the COUNT over the same union and filters as the list query.
func buildModelCountQuery(f *ModelFilters, componentTypes []string) (string, []interface{}) {
	where, args := buildModelWhere(f, componentTypes)

	return "SELECT COUNT(*) FROM (" + modelUnionQuery + ") m" + where, args
}

// buildModelGetQuery builds the lookup of one model over the same union as the list, limited to
// the model component types when they are given.
func buildModelGetQuery(id uuid.UUID, componentTypes []string) (string, []interface{}) {
	query := "SELECT " + modelColumns + " FROM (" + modelUnionQuery + ") m WHERE id = $1"
	args := []interface{}{id}

	if componentTypes != nil {
		query += " AND component_type = ANY($2)"
		args = append(args, componentTypes)
	}

	return query, args
}

// GetByID returns the model with the given id, whoever created it.
func (r *modelRepo) GetByID(ctx context.Context, id uuid.UUID, componentTypes []string) (*ModelRow, error) {
	query, args := buildModelGetQuery(id, componentTypes)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("failed to get model: %w", err)
		}

		return nil, nil
	}

	m, err := scanModelRow(rows)
	if err != nil {
		return nil, err
	}

	return &m, nil
}

// GetCount returns the total number of models matching the filters.
func (r *modelRepo) GetCount(ctx context.Context, filters *ModelFilters, componentTypes []string) (int, error) {
	query, args := buildModelCountQuery(filters, componentTypes)

	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("failed to count models: %w", err)
	}

	return total, nil
}

// List returns one page of models matching the filters.
func (r *modelRepo) List(ctx context.Context, filters *ModelFilters, componentTypes []string) ([]ModelRow, error) {
	query, args := buildModelListQuery(filters, componentTypes)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer rows.Close()

	var models []ModelRow

	for rows.Next() {
		m, err := scanModelRow(rows)
		if err != nil {
			return nil, err
		}

		models = append(models, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating models: %w", err)
	}

	return models, nil
}

// scanModelRow scans one row projected from modelColumns.
func scanModelRow(rows pgx.Rows) (ModelRow, error) {
	var (
		m                    ModelRow
		name, message        sql.NullString
		workerID, workerName sql.NullString
		workerRuntime        sql.NullString
		createdBy            sql.NullString
		paramsJSON           []byte
	)

	if err := rows.Scan(
		&m.ID, &m.DeploymentType, &name, &m.ComponentType, &m.Provider, &m.Model, &paramsJSON,
		&m.Status, &message, &workerID, &workerName, &workerRuntime, &createdBy, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return m, fmt.Errorf("failed to scan model: %w", err)
	}

	m.Name = name.String
	m.Message = message.String
	m.WorkerID = workerID.String
	m.WorkerName = workerName.String
	m.WorkerRuntimeType = workerRuntime.String
	m.CreatedBy = createdBy.String

	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &m.Params); err != nil {
			return m, fmt.Errorf("failed to unmarshal model params: %w", err)
		}
	}

	return m, nil
}
