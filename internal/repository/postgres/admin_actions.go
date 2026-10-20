package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/lib/pq"
)

type AdminActionRepository struct {
	db *sql.DB
}

func NewAdminActionRepository(db *sql.DB) *AdminActionRepository {
	return &AdminActionRepository{db: db}
}

func (r *AdminActionRepository) CancelBooking(ctx context.Context, bookingID string, audit *domain.AuditEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.ErrorContext(ctx, "Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	result, err := tx.ExecContext(ctx, `UPDATE bookings SET status = $1 WHERE id = $2 AND status != $1`, string(domain.StatusCancelled), bookingID)
	if err != nil {
		if isInvalidUUID(err) {
			return domain.ErrBookingNotFound
		}
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domain.ErrBookingNotFound
	}
	if err := insertAuditEntry(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminActionRepository) CreateBranch(ctx context.Context, branch *domain.Branch, serviceIDs []string, audit *domain.AuditEntry) (*domain.Branch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.ErrorContext(ctx, "Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO branches (name, address, city, phone, hours, capacity) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		branch.Name, branch.Address, branch.City, branch.Phone, branch.Hours, branch.Capacity,
	).Scan(&branch.ID, &branch.CreatedAt)
	if err != nil {
		return nil, err
	}
	for _, serviceID := range serviceIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO branch_services (branch_id, service_id) VALUES ($1, $2)`, branch.ID, serviceID); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23503" && pqErr.Constraint == "branch_services_service_id_fkey" {
				return nil, domain.ErrServiceNotFound
			}
			return nil, err
		}
	}
	audit.TargetID = branch.ID
	if err := insertAuditEntry(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return branch, nil
}

func (r *AdminActionRepository) DeleteBranch(ctx context.Context, id string, audit *domain.AuditEntry) (int, error) {
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
	audit.Details = fmt.Sprintf("bookings_preserved=%d", bookingsPreserved)
	if err := insertAuditEntry(ctx, tx, audit); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return bookingsPreserved, nil
}

func insertAuditEntry(ctx context.Context, tx *sql.Tx, entry *domain.AuditEntry) error {
	return tx.QueryRowContext(ctx,
		`INSERT INTO admin_audit_log (admin_id, action, target_type, target_id, details, request_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		entry.AdminID, entry.Action, entry.TargetType, entry.TargetID, entry.Details, entry.RequestID,
	).Scan(&entry.ID, &entry.CreatedAt)
}
