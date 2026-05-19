package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

type ApplicationRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, message string) error
	GetServicesByAppID(ctx context.Context, appID uuid.UUID) ([]models.Service, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type applicationRepo struct {
	pool *pgxpool.Pool
}

func NewApplicationRepository(pool *pgxpool.Pool) ApplicationRepository {
	return &applicationRepo{pool: pool}
}

func (r *applicationRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error) {
	query := `SELECT id, name, template, status, message, created_by, created_at, updated_at
              FROM applications WHERE id = $1`

	var app models.Application
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&app.ID, &app.Name, &app.Template, &app.Status,
		&app.Message, &app.CreatedBy, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("not found: application %s: %w", id, err)
	}

	return &app, nil
}

func (r *applicationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string, message string) error {
	query := `UPDATE applications SET status=$2, message=$3 WHERE id=$1`

	_, err := r.pool.Exec(ctx, query, id, status, message)
	if err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}

	return nil
}

func (r *applicationRepo) GetServicesByAppID(ctx context.Context, appID uuid.UUID) ([]models.Service, error) {
	query := `SELECT id, app_id, type, status, endpoints, version, created_at, updated_at
              FROM services WHERE app_id = $1`

	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %w", err)
	}
	defer rows.Close()

	var services []models.Service
	for rows.Next() {
		var s models.Service
		if err := rows.Scan(&s.ID, &s.AppID, &s.Type, &s.Status,
			&s.Endpoints, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan service: %w", err)
		}
		services = append(services, s)
	}

	return services, rows.Err()
}

func (r *applicationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM applications WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	return nil
}
