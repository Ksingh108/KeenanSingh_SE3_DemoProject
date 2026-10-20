// Package postgres contains Postgres-backed implementations of repository
// interfaces. Implementations are intentionally thin and focused on SQL
// interactions and mapping results into domain models.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/gringottsbankbranchbooking/internal/domain"
)

// BookingRepository provides persistence operations for bookings using a
// Postgres database connection.
type BookingRepository struct {
	db *sql.DB
}

// NewBookingRepository returns a new BookingRepository using the provided DB
// connection (connection pool). The caller is responsible for DB lifecycle.
func NewBookingRepository(db *sql.DB) *BookingRepository {
	return &BookingRepository{db: db}
}

func scanBookings(ctx context.Context, rows *sql.Rows, scanOperation, iterateOperation string) ([]*domain.Booking, error) {
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	var bookings []*domain.Booking
	for rows.Next() {
		booking := &domain.Booking{}
		var branchName, serviceName string
		if err := rows.Scan(
			&booking.ID,
			&booking.BranchID,
			&booking.ServiceID,
			&booking.CustomerName,
			&booking.Email,
			&booking.Phone,
			&booking.Date,
			&booking.TimeSlot,
			&booking.Status,
			&booking.CreatedAt,
			&branchName,
			&serviceName,
		); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", scanOperation, "error", err.Error())
			return nil, err
		}
		booking.Branch = &domain.BranchRef{ID: booking.BranchID, Name: branchName}
		booking.Service = &domain.ServiceRef{ID: booking.ServiceID, Name: serviceName}
		bookings = append(bookings, booking)
	}

	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", iterateOperation, "error", err.Error())
		return nil, err
	}

	return bookings, nil
}

// CreateWithCapacityCheck atomically checks capacity and creates a booking in a transaction.
//
// Concurrency: the transaction first locks the booking's branch row with
// SELECT...FOR UPDATE, so concurrent bookings for the same branch run one at a
// time and each sees the previous one's committed insert when it counts.
// Locking the existing booking rows instead would not work: an empty slot has
// no rows to lock, so concurrent first bookings could all pass the check and
// overbook it (PostgreSQL's default READ COMMITTED isolation does not prevent
// this). The cost is that bookings for one branch are serialised, including
// across different slots and dates; if that becomes a bottleneck, a per-slot
// advisory lock (pg_advisory_xact_lock) narrows the contention.
// Cancelling a booking does not take this lock, so it is never blocked by it.
//
// Returns domain.ErrCapacityExceeded if the time slot is at or over capacity,
// or domain.ErrBranchNotFound if the branch does not exist.
func (r *BookingRepository) CreateWithCapacityCheck(ctx context.Context, booking *domain.Booking) error {
	return r.createWithCapacityCheck(ctx, booking, "", "")
}

func (r *BookingRepository) CreateWithCapacityCheckIdempotent(ctx context.Context, booking *domain.Booking, key, fingerprint string) error {
	return r.createWithCapacityCheck(ctx, booking, key, fingerprint)
}

func (r *BookingRepository) createWithCapacityCheck(ctx context.Context, booking *domain.Booking, idempotencyKey, fingerprint string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Start a transaction for atomic check-then-insert with row locking
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "begin transaction for capacity check", "error", err.Error())
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.ErrorContext(ctx, "Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	if idempotencyKey != "" {
		// Serialises concurrent requests with the same key until this transaction ends.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, idempotencyKey); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "lock idempotency key", "error", err.Error())
			return err
		}

		existing := &domain.Booking{}
		var branchName, serviceName, existingFingerprint string
		err := tx.QueryRowContext(ctx, `
			SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone,
			       to_char(b.date, 'YYYY-MM-DD'), b.time_slot, b.status, b.created_at, br.name, s.name, b.request_fingerprint
			FROM bookings b
		JOIN branches br ON br.id = b.branch_id
		JOIN services s ON s.id = b.service_id
		WHERE b.idempotency_key = $1`, idempotencyKey).Scan(
			&existing.ID, &existing.BranchID, &existing.ServiceID, &existing.CustomerName,
			&existing.Email, &existing.Phone, &existing.Date, &existing.TimeSlot,
			&existing.Status, &existing.CreatedAt, &branchName, &serviceName, &existingFingerprint,
		)
		if err == nil {
			if existingFingerprint != fingerprint {
				return domain.ErrIdempotencyKeyReused
			}
			if existing.Status == string(domain.StatusCancelled) {
				return domain.ErrIdempotentBookingCancelled
			}
			existing.Branch = &domain.BranchRef{ID: existing.BranchID, Name: branchName}
			existing.Service = &domain.ServiceRef{ID: existing.ServiceID, Name: serviceName}
			*booking = *existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			slog.ErrorContext(ctx, "Operation failed", "operation", "find booking by idempotency key", "error", err.Error())
			return err
		}
	}

	// Serialise concurrent bookings for this branch. The branch row always
	// exists, even when the slot has no bookings yet, so this lock closes the
	// gap that locking booking rows would leave. It is held until commit or
	// rollback.
	var branchCapacity int
	err = tx.QueryRowContext(ctx, `SELECT capacity FROM branches WHERE id = $1 AND is_active = TRUE FOR UPDATE`, booking.BranchID).Scan(&branchCapacity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isInvalidUUID(err) {
			return domain.ErrBranchNotFound
		}
		slog.ErrorContext(ctx, "Operation failed", "operation", "lock branch for capacity check", "error", err.Error())
		return err
	}

	// Count under the lock, so no other booking for this branch can be
	// inserted between this count and our insert.
	var currentCount int
	countQuery := `SELECT COUNT(*) FROM bookings WHERE branch_id = $1 AND date = $2 AND time_slot = $3 AND status != $4`
	err = tx.QueryRowContext(ctx, countQuery, booking.BranchID, booking.Date, booking.TimeSlot, string(domain.StatusCancelled)).Scan(&currentCount)
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "check booking count in transaction", "error", err.Error())
		return err
	}

	// Check capacity now that the count is stable
	if currentCount >= branchCapacity {
		return domain.ErrCapacityExceeded
	}

	// Set booking metadata before persisting
	if booking.ID == "" {
		booking.ID = uuid.New().String()
	}
	booking.CreatedAt = time.Now()
	booking.Status = string(domain.StatusConfirmed)

	// Insert the booking within the same transaction
	insertQuery := `
		INSERT INTO bookings (id, branch_id, service_id, customer_name, email, phone, date, time_slot, status, created_at, idempotency_key, request_fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), NULLIF($12, ''))
	`

	_, err = tx.ExecContext(ctx, insertQuery,
		booking.ID,
		booking.BranchID,
		booking.ServiceID,
		booking.CustomerName,
		booking.Email,
		booking.Phone,
		booking.Date,
		booking.TimeSlot,
		booking.Status,
		booking.CreatedAt,
		idempotencyKey,
		fingerprint,
	)
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "insert booking in transaction", "error", err.Error())
		return err
	}

	// Commit the transaction to release locks and persist changes
	if err := tx.Commit(); err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "commit transaction", "error", err.Error())
		return err
	}

	return nil
}

func (r *BookingRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Booking, string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	booking := &domain.Booking{}
	var branchName, serviceName, fingerprint string
	err := r.db.QueryRowContext(ctx, `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone,
		       to_char(b.date, 'YYYY-MM-DD'), b.time_slot, b.status, b.created_at, br.name, s.name, b.request_fingerprint
		FROM bookings b
		JOIN branches br ON br.id = b.branch_id
		JOIN services s ON s.id = b.service_id
		WHERE b.idempotency_key = $1`, key).Scan(
		&booking.ID, &booking.BranchID, &booking.ServiceID, &booking.CustomerName,
		&booking.Email, &booking.Phone, &booking.Date, &booking.TimeSlot,
		&booking.Status, &booking.CreatedAt, &branchName, &serviceName, &fingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "get booking by idempotency key", "error", err.Error())
		return nil, "", false, err
	}
	booking.Branch = &domain.BranchRef{ID: booking.BranchID, Name: branchName}
	booking.Service = &domain.ServiceRef{ID: booking.ServiceID, Name: serviceName}
	return booking, fingerprint, true, nil
}

// GetAllPaginated returns bookings with pagination applied.
// limit is the max number of results per page, offset is the start position.
func (r *BookingRepository) GetAllPaginated(ctx context.Context, limit int, offset int) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, to_char(b.date, 'YYYY-MM-DD'), b.time_slot, b.status, b.created_at,
			   br.name as branch_name, s.name as service_name
		FROM bookings b
		JOIN branches br ON b.branch_id = br.id
		JOIN services s ON b.service_id = s.id
		ORDER BY b.date DESC, b.time_slot DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "get paginated bookings from database", "error", err.Error())
		return nil, err
	}
	return scanBookings(ctx, rows, "scan booking row", "iterate booking rows")
}

// GetByEmailPaginated returns bookings for a specific email with pagination.
// limit is the max number of results per page, offset is the start position.
func (r *BookingRepository) GetByEmailPaginated(ctx context.Context, email string, limit int, offset int) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, to_char(b.date, 'YYYY-MM-DD'), b.time_slot, b.status, b.created_at,
			   br.name as branch_name, s.name as service_name
		FROM bookings b
		JOIN branches br ON b.branch_id = br.id
		JOIN services s ON b.service_id = s.id
		WHERE b.email = $1
		ORDER BY b.date DESC, b.time_slot DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, email, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "get paginated bookings by email from database", "error", err.Error())
		return nil, err
	}
	return scanBookings(ctx, rows, "scan booking row by email", "iterate booking rows by email")
}

// GetBookedSlots returns a map of booked time slots and their counts for a specific
// branch and date. Uses a single GROUP BY query instead of querying each slot individually.
// This is more efficient for availability checking across all time slots.
func (r *BookingRepository) GetBookedSlots(ctx context.Context, branchID, date string) (map[string]int, error) {
	query := `
		SELECT time_slot, COUNT(*) as count
		FROM bookings
		WHERE branch_id = $1 AND date = $2 AND status != $3
		GROUP BY time_slot
	`

	rows, err := r.db.QueryContext(ctx, query, branchID, date, string(domain.StatusCancelled))
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "get booked slots from database", "error", err.Error())
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	slots := make(map[string]int)
	for rows.Next() {
		var timeSlot string
		var count int
		err := rows.Scan(&timeSlot, &count)
		if err != nil {
			slog.ErrorContext(ctx, "Operation failed", "operation", "scan booked slot row", "error", err.Error())
			return nil, err
		}
		slots[timeSlot] = count
	}

	if err = rows.Err(); err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "iterate booked slot rows", "error", err.Error())
		return nil, err
	}

	return slots, nil
}

// Cancel marks the booking status as cancelled. If no rows are affected the
// function returns domain.ErrBookingNotFound so callers can handle the case
// where the booking does not exist or is already cancelled.
func (r *BookingRepository) Cancel(ctx context.Context, bookingID string) error {
	query := `UPDATE bookings SET status = $1 WHERE id = $2 AND status != $1`
	result, err := r.db.ExecContext(ctx, query, string(domain.StatusCancelled), bookingID)
	if err != nil {
		if isInvalidUUID(err) {
			return domain.ErrBookingNotFound
		}
		slog.ErrorContext(ctx, "Operation failed", "operation", "cancel booking in database", "error", err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		slog.ErrorContext(ctx, "Operation failed", "operation", "get rows affected for booking cancellation", "error", err.Error())
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBookingNotFound
	}

	return nil
}
