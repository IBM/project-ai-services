package repository

import (
	"context"
	"database/sql"
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
	// Upsert inserts a new worker or updates its runtime_type and registered_at on name conflict.
	Upsert(ctx context.Context, worker *models.Worker) error
	// Update applies a partial update to the fields set in WorkerUpdate; nil fields are left unchanged.
	Update(ctx context.Context, name string, update WorkerUpdate) error
	// Delete removes a worker by ID.
	Delete(ctx context.Context, id uuid.UUID) error
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
// and timestamps. ID, RegisteredAt, and UpdatedAt are populated via RETURNING.
func (r *workerRepo) Upsert(ctx context.Context, worker *models.Worker) error {
	query := `
		INSERT INTO workers (name, runtime_type, status)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE
			SET runtime_type   = EXCLUDED.runtime_type,
			    status         = EXCLUDED.status,
			    registered_at  = NOW(),
			    updated_at     = NOW()
		RETURNING id, registered_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query,
		worker.Name,
		worker.RuntimeType,
		worker.Status,
	).Scan(&worker.ID, &worker.RegisteredAt, &worker.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert worker: %w", err)
	}

	return nil
}

// Update performs a partial update on a worker row.
// Nil fields in WorkerUpdate are left unchanged via COALESCE. updated_at is always refreshed.
func (r *workerRepo) Update(ctx context.Context, name string, update WorkerUpdate) error {
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
		WHERE name = $3
	`

	_, err := r.pool.Exec(ctx, query, statusArg, hb, name)
	if err != nil {
		return fmt.Errorf("failed to update worker %q: %w", name, err)
	}

	return nil
}

// Delete removes a worker by ID.
func (r *workerRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM workers WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete worker %q: %w", id, err)
	}

	return nil
}

// GetAll returns all worker rows ordered by registered_at ascending.
func (r *workerRepo) GetAll(ctx context.Context) ([]models.Worker, error) {
	query := `
		SELECT id, name, runtime_type, status, last_heartbeat, registered_at, updated_at
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
			w  models.Worker
			hb sql.NullTime
		)

		if err := rows.Scan(
			&w.ID, &w.Name, &w.RuntimeType, &w.Status,
			&hb, &w.RegisteredAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan worker row: %w", err)
		}

		if hb.Valid {
			w.LastHeartbeat = &hb.Time
		}

		workers = append(workers, w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating worker rows: %w", err)
	}

	return workers, nil
}

// Made with Bob
