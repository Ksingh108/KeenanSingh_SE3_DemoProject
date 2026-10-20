// Package postgres contains Postgres-backed implementations of repository
// interfaces. Implementations focus on SQL interactions and mapping results
// into domain models.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/lib/pq"
)

// BranchRepository provides persistence operations for branches using a
// Postgres database connection.
type BranchRepository struct {
	db *sql.DB
}

// NewBranchRepository constructs a new BranchRepository. The caller should
// provide a properly configured *sql.DB (connection pool).
func NewBranchRepository(db *sql.DB) *BranchRepository {
	return &BranchRepository{db: db}
}

// GetAll returns all branches and their associated services.
func (r *BranchRepository) GetAll(ctx context.Context) ([]*domain.Branch, error) {
	query := `SELECT id, name, address, city, phone, hours, capacity, created_at FROM branches WHERE is_active = TRUE ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// Ensure rows are closed to release DB resources back to the pool.
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close branch rows", "error", err.Error())
		}
	}()

	var branches []*domain.Branch
	branchesByID := make(map[string]*domain.Branch)
	branchIDs := make([]string, 0)
	for rows.Next() {
		branch := &domain.Branch{}
		// Scan columns into the domain model fields. Order must match SELECT.
		err := rows.Scan(&branch.ID, &branch.Name, &branch.Address, &branch.City, &branch.Phone, &branch.Hours, &branch.Capacity, &branch.CreatedAt)
		if err != nil {
			return nil, err
		}

		branches = append(branches, branch)
		branchesByID[branch.ID] = branch
		branchIDs = append(branchIDs, branch.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	serviceQuery := `
		SELECT bs.branch_id, s.id, s.name, s.description, s.created_at
		FROM services s
		JOIN branch_services bs ON s.id = bs.service_id
		WHERE bs.branch_id = ANY($1)
		ORDER BY bs.branch_id, s.name`
	serviceRows, err := r.db.QueryContext(ctx, serviceQuery, pq.Array(branchIDs))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := serviceRows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close branch service rows", "error", err.Error())
		}
	}()

	for serviceRows.Next() {
		var branchID string
		service := &domain.Service{}
		if err := serviceRows.Scan(&branchID, &service.ID, &service.Name, &service.Description, &service.CreatedAt); err != nil {
			return nil, err
		}
		branchesByID[branchID].Services = append(branchesByID[branchID].Services, service)
	}
	return branches, serviceRows.Err()
}

// GetByID returns a branch by its ID, including associated services. If the
// branch does not exist the function returns domain.ErrBranchNotFound.
func (r *BranchRepository) GetByID(ctx context.Context, id string) (*domain.Branch, error) {
	query := `SELECT id, name, address, city, phone, hours, capacity, created_at FROM branches WHERE id = $1 AND is_active = TRUE`
	branch := &domain.Branch{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&branch.ID, &branch.Name, &branch.Address, &branch.City, &branch.Phone, &branch.Hours, &branch.Capacity, &branch.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrBranchNotFound
		}
		return nil, err
	}

	// Load services for this branch and attach them to the result.
	services, err := r.GetServices(ctx, branch.ID)
	if err != nil {
		return nil, err
	}
	branch.Services = services

	return branch, nil
}

// GetServices returns services associated with a given branch ID. The
// function maps rows into domain.Service objects and returns any iteration
// errors encountered during scanning.
func (r *BranchRepository) GetServices(ctx context.Context, branchID string) ([]*domain.Service, error) {
	query := `
		SELECT s.id, s.name, s.description, s.created_at 
		FROM services s
		JOIN branch_services bs ON s.id = bs.service_id
		WHERE bs.branch_id = $1
		ORDER BY s.name`

	rows, err := r.db.QueryContext(ctx, query, branchID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close branch service rows", "error", err.Error())
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

// Create inserts a new branch and associates it with the provided service
// IDs within a transaction. If any step fails the transaction is rolled
// back to avoid partial state.
func (r *BranchRepository) Create(ctx context.Context, branch *domain.Branch, serviceIDs []string) (*domain.Branch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Ensure rollback unless commit succeeds. Rollback on a committed tx is a
	// no-op so deferring it is safe.
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.ErrorContext(ctx, "Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	// Insert the branch and return generated fields like id and created_at.
	query := `INSERT INTO branches (name, address, city, phone, hours, capacity) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`
	err = tx.QueryRowContext(ctx, query, branch.Name, branch.Address, branch.City, branch.Phone, branch.Hours, branch.Capacity).Scan(&branch.ID, &branch.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Associate services with the newly created branch.
	for _, serviceID := range serviceIDs {
		_, err = tx.ExecContext(ctx, `INSERT INTO branch_services (branch_id, service_id) VALUES ($1, $2)`, branch.ID, serviceID)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return branch, nil
}

// Delete archives a branch only when it has no live bookings attached. Canceled
// bookings remain associated with the archived branch for historical records.
func (r *BranchRepository) Delete(ctx context.Context, id string) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.ErrorContext(ctx, "Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	bookingsPreserved, err := archiveBranch(ctx, tx, id)
	if err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return bookingsPreserved, nil
}

func archiveBranch(ctx context.Context, tx *sql.Tx, id string) (int, error) {
	var lockedID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM branches WHERE id = $1 AND is_active = TRUE FOR UPDATE`, id).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isInvalidUUID(err) {
			return 0, domain.ErrBranchNotFound
		}
		return 0, err
	}

	var activeBookings, totalBookings int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE status != $2), COUNT(*) FROM bookings WHERE branch_id = $1`,
		id, string(domain.StatusCancelled),
	).Scan(&activeBookings, &totalBookings)
	if err != nil {
		return 0, err
	}
	if activeBookings > 0 {
		return 0, domain.ErrBranchDeleteBlockedByBookings
	}
	if _, err := tx.ExecContext(ctx, `UPDATE branches SET is_active = FALSE WHERE id = $1`, id); err != nil {
		return 0, err
	}
	return totalBookings, nil
}

// isInvalidUUID reports whether err is Postgres rejecting a value as a
// malformed UUID (SQLSTATE 22P02). IDs are validated for allowed characters
// only, so a well-formed but non-UUID ID can reach the query; no branch can
// have such an ID, so callers treat it as "not found".
func isInvalidUUID(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "22P02"
}
