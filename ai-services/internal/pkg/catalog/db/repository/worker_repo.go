package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// WorkerUpdate carries the fields that may be changed by Update.
// Nil fields are left unchanged.
type WorkerUpdate struct {
	Status        *models.WorkerStatus
	LastHeartbeat *time.Time
}

// WorkerRepository defines the interface for worker data operations.
type WorkerRepository interface {
	// Upsert inserts a new worker or updates its runtime_type, status, and metadata on name conflict.
	Upsert(ctx context.Context, worker *models.Worker) error
	// Update applies a partial update to the fields set in WorkerUpdate; nil fields are left unchanged.
	Update(ctx context.Context, id uuid.UUID, update WorkerUpdate) error
	// Delete removes a worker by ID. Returns (false, nil) if no row matched.
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	// GetAll returns all worker rows ordered by registered_at ascending.
	GetAll(ctx context.Context) ([]models.Worker, error)
}

// workerRepo implements WorkerRepository using pgx.
type workerRepo struct {
	pool *pgxpool.Pool
}

// NewWorkerRepository creates a new WorkerRepository instance.
func NewWorkerRepository(pool *pgxpool.Pool) WorkerRepository {
	return &workerRepo{pool: pool}
}

// Upsert inserts a worker or, on name conflict, updates runtime_type, status,
// metadata, and timestamps. ID, RegisteredAt, and UpdatedAt are populated via RETURNING.
func (r *workerRepo) Upsert(ctx context.Context, worker *models.Worker) error {
	var metadataJSON []byte
	if worker.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(worker.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal worker metadata: %w", err)
		}
	}

	query := `
		INSERT INTO workers (name, runtime_type, status, metadata)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE
			SET runtime_type   = EXCLUDED.runtime_type,
			    status         = EXCLUDED.status,
			    metadata       = EXCLUDED.metadata,
			    registered_at  = NOW(),
			    updated_at     = NOW()
		RETURNING id, registered_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query,
		worker.Name,
		worker.RuntimeType,
		worker.Status,
		metadataJSON,
	).Scan(&worker.ID, &worker.RegisteredAt, &worker.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert worker: %w", err)
	}

	return nil
}

// Update performs a partial update on a worker row.
// Nil fields in WorkerUpdate are left unchanged via COALESCE. updated_at is always refreshed.
func (r *workerRepo) Update(ctx context.Context, id uuid.UUID, update WorkerUpdate) error {
	var hb sql.NullTime
	if update.LastHeartbeat != nil {
		hb = sql.NullTime{Time: *update.LastHeartbeat, Valid: true}
	}

	var statusArg any
	if update.Status != nil {
		statusArg = *update.Status
	}

	query := `
		UPDATE workers
		SET status         = COALESCE($1, status),
		    last_heartbeat = COALESCE($2, last_heartbeat),
		    updated_at     = NOW()
		WHERE id = $3
	`

	_, err := r.pool.Exec(ctx, query, statusArg, hb, id)
	if err != nil {
		return fmt.Errorf("failed to update worker %q: %w", id, err)
	}

	return nil
}

// Delete removes a worker by ID.
// Returns (true, nil) if the row was deleted, (false, nil) if no row matched.
func (r *workerRepo) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	query := `DELETE FROM workers WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("failed to delete worker %q: %w", id, err)
	}

	return tag.RowsAffected() > 0, nil
}

// GetAll returns all worker rows ordered by registered_at ascending.
func (r *workerRepo) GetAll(ctx context.Context) ([]models.Worker, error) {
	query := `
		SELECT id, name, runtime_type, status, last_heartbeat, metadata, registered_at, updated_at
		FROM workers
		ORDER BY registered_at ASC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query workers: %w", err)
	}
	defer rows.Close()

	var workers []models.Worker

	for rows.Next() {
		var (
			w            models.Worker
			hb           sql.NullTime
			metadataJSON []byte
		)

		if err := rows.Scan(
			&w.ID, &w.Name, &w.RuntimeType, &w.Status,
			&hb, &metadataJSON, &w.RegisteredAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan worker row: %w", err)
		}

		if hb.Valid {
			w.LastHeartbeat = &hb.Time
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &w.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal worker metadata: %w", err)
			}
		}

		workers = append(workers, w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating worker rows: %w", err)
	}

	return workers, nil
}

// Made with Bob
