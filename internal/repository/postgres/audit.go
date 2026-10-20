package postgres

import (
	"context"
	"database/sql"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

// AuditRepository appends admin audit entries to the admin_audit_log table.
type AuditRepository struct {
	db *sql.DB
}

// NewAuditRepository creates a new AuditRepository with the provided DB
// connection (pool).
func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Create appends an audit entry and fills in the generated id and created_at.
// Entries are append-only; this repository deliberately offers no update or
// delete.
func (r *AuditRepository) Create(ctx context.Context, entry *domain.AuditEntry) error {
	query := `
		INSERT INTO admin_audit_log (admin_id, action, target_type, target_id, details, request_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	return r.db.QueryRowContext(ctx, query,
		entry.AdminID, entry.Action, entry.TargetType, entry.TargetID, entry.Details, entry.RequestID,
	).Scan(&entry.ID, &entry.CreatedAt)
}
