// Package postgres implements repository interfaces backed by Postgres. The
// implementations translate between SQL rows and domain models and should be
// kept thin — business logic belongs in the service layer.
package postgres

import (
	"context"
	"database/sql"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

// SessionRepository manages admin session persistence (login tokens).
type SessionRepository struct {
	db *sql.DB
}

// NewSessionRepository creates a new SessionRepository with the provided DB
// connection (pool).
func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create inserts a new admin session (token_hash, admin_id, expires_at). The
// method returns any error from the database driver.
func (r *SessionRepository) Create(ctx context.Context, session *domain.AdminSession) error {
	query := `INSERT INTO admin_sessions (token_hash, admin_id, expires_at) VALUES ($1, $2, $3)`
	_, err := r.db.ExecContext(ctx, query, session.TokenHash, session.AdminID, session.ExpiresAt)
	return err
}

// GetByTokenHash retrieves a non-expired admin session by token hash. The query
// ensures the session hasn't expired by comparing expires_at to NOW().
// If no row is found QueryRowContext.Scan will return sql.ErrNoRows which
// callers should handle appropriately.
func (r *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.AdminSession, error) {
	query := `SELECT token_hash, admin_id, expires_at FROM admin_sessions WHERE token_hash = $1 AND expires_at > NOW()`

	session := &domain.AdminSession{}
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&session.TokenHash,
		&session.AdminID,
		&session.ExpiresAt,
	)

	if err != nil {
		return nil, err
	}

	return session, nil
}

// Delete removes a session by token hash. The function returns any execution
// error from the database. Callers should handle the case where no rows
// were affected if they need to know whether a session existed.
func (r *SessionRepository) Delete(ctx context.Context, tokenHash string) error {
	query := `DELETE FROM admin_sessions WHERE token_hash = $1`
	_, err := r.db.ExecContext(ctx, query, tokenHash)
	return err
}

// CleanupExpired removes all sessions that have expired. This can be used
// as part of periodic cleanup jobs to keep the sessions table small.
func (r *SessionRepository) CleanupExpired(ctx context.Context) error {
	query := `DELETE FROM admin_sessions WHERE expires_at <= NOW()`
	_, err := r.db.ExecContext(ctx, query)
	return err
}
