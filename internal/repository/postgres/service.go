// Package postgres provides Postgres-backed repository implementations. The
// repositories focus on SQL interactions and mapping results into domain
// models.
package postgres

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

// ServiceRepository handles persistence operations for service entities.
type ServiceRepository struct {
	db *sql.DB
}

// NewServiceRepository creates a ServiceRepository with the provided DB
// connection. The DB should be a configured connection pool.
func NewServiceRepository(db *sql.DB) *ServiceRepository {
	return &ServiceRepository{db: db}
}

// GetAll returns all services from the database ordered by name. It maps
// SQL rows into domain.Service objects and returns any scanning or iteration
// errors encountered.
func (r *ServiceRepository) GetAll(ctx context.Context) ([]*domain.Service, error) {
	query := `SELECT id, name, description, created_at FROM services ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// Ensure rows are closed to free DB resources.
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close service rows", "error", err.Error())
		}
	}()

	var services []*domain.Service
	for rows.Next() {
		service := &domain.Service{}
		if err := rows.Scan(&service.ID, &service.Name, &service.Description, &service.CreatedAt); err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return services, rows.Err()
}
