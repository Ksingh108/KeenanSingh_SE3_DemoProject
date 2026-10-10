// Package postgres contains Postgres-backed implementations of repository
// interfaces. These implementations should be focused on persistence and
// translating between SQL rows and domain models.
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// AdminRepository implements admin-related persistence using a Postgres DB
// connection. It performs parameterized SQL queries and maps results to the
// domain.Admin model.
type AdminRepository struct {
	db *sql.DB
}

// NewAdminRepository constructs a new AdminRepository with the provided DB
// connection. The caller is responsible for providing a properly configured
// *sql.DB (connection pool) instance.
func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

// GetByUsername looks up an admin by username. It returns a populated
// domain.Admin on success, nil and an error if the user is not found or if
// the query fails.
//
// Behavior and error semantics:
//   - If no row is found the function returns a wrapped error ("user not found").
//     Callers may want to compare the error message or wrap this in a package
//     error constant for better checks.
//   - Any other SQL error is returned directly to the caller for higher-level
//     handling.
//
// The query uses a parameter placeholder ($1) to avoid SQL injection and let
// the database driver handle proper escaping/binding.
func (r *AdminRepository) GetByUsername(ctx context.Context, username string) (*domain.Admin, error) {
	// Prepare a domain object to receive scanned values.
	admin := &domain.Admin{}

	// Parameterized query to select the admin by username. The selected
	// columns must match the order of Scan arguments below.
	query := `SELECT id, username, password_hash, created_at FROM admins WHERE username = $1`

	// QueryRowContext is used for single-row queries with context cancellation.
	// Scan will return sql.ErrNoRows if nothing is found.
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&admin.ID, &admin.Username, &admin.PasswordHash, &admin.CreatedAt,
	)
	if err != nil {
		// Translate a no-row result into a readable error. Consider replacing
		// this with a package-level sentinel error (e.g. ErrAdminNotFound)
		// if callers need to detect this case programmatically.
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found")
		}
		// Propagate other SQL errors up for the service layer to handle.
		return nil, err
	}

	// Success: return the populated domain.Admin.
	return admin, nil
}
