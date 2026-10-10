// Package postgres contains Postgres-backed implementations of repository
// interfaces. Implementations focus on SQL interactions and mapping results
// into domain models.
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/GringottsBankBranchBooking/internal/domain"
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

// GetAll returns all branches and their associated services. The function
// performs a separate query to load services for each branch
func (r *BranchRepository) GetAll(ctx context.Context) ([]*domain.Branch, error) {
	query := `SELECT id, name, address, city, phone, hours, capacity, created_at FROM branches ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// Ensure rows are closed to release DB resources back to the pool.
	defer rows.Close()

	var branches []*domain.Branch
	for rows.Next() {
		branch := &domain.Branch{}
		// Scan columns into the domain model fields. Order must match SELECT.
		err := rows.Scan(&branch.ID, &branch.Name, &branch.Address, &branch.City, &branch.Phone, &branch.Hours, &branch.Capacity, &branch.CreatedAt)
		if err != nil {
			return nil, err
		}

		// Load services for this branch. This keeps the repository API simple
		// but does an additional query per branch.
		services, err := r.GetServices(ctx, branch.ID)
		if err != nil {
			return nil, err
		}
		branch.Services = services

		branches = append(branches, branch)
	}
	return branches, rows.Err()
}

// GetByID returns a branch by its ID, including associated services. If the
// branch does not exist the function returns domain.ErrBranchNotFound.
func (r *BranchRepository) GetByID(ctx context.Context, id string) (*domain.Branch, error) {
	query := `SELECT id, name, address, city, phone, hours, capacity, created_at FROM branches WHERE id = $1`
	branch := &domain.Branch{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&branch.ID, &branch.Name, &branch.Address, &branch.City, &branch.Phone, &branch.Hours, &branch.Capacity, &branch.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
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
	defer rows.Close()

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
	defer tx.Rollback()

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

// Delete removes a branch by ID and returns how many bookings were removed
// with it, or domain.ErrBranchNotFound if no branch matched. Because
// bookings.branch_id is ON DELETE CASCADE the bookings are deleted by the
// database, so they are counted first. The branch row is locked for the
// duration: inserting a booking takes a conflicting lock on its parent branch
// row, so no booking can slip in between the count and the delete.
func (r *BranchRepository) Delete(ctx context.Context, id string) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var lockedID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM branches WHERE id = $1 FOR UPDATE`, id).Scan(&lockedID)
	if err != nil {
		if err == sql.ErrNoRows || isInvalidUUID(err) {
			return 0, domain.ErrBranchNotFound
		}
		return 0, err
	}

	var bookingsRemoved int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookings WHERE branch_id = $1`, id).Scan(&bookingsRemoved)
	if err != nil {
		return 0, err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM branches WHERE id = $1`, id); err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return bookingsRemoved, nil
}

// isInvalidUUID reports whether err is Postgres rejecting a value as a
// malformed UUID (SQLSTATE 22P02). IDs are validated for allowed characters
// only, so a well-formed but non-UUID ID can reach the query; no branch can
// have such an ID, so callers treat it as "not found".
func isInvalidUUID(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "22P02"
}
