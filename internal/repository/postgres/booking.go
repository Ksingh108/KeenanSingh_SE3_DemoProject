// Package postgres contains Postgres-backed implementations of repository
// interfaces. Implementations are intentionally thin and focused on SQL
// interactions and mapping results into domain models.
package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/google/uuid"
	"log/slog"
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

// Create inserts a new booking row into the database. If the booking does
// not have an ID, a UUID is generated. The function also sets CreatedAt and
// an initial status before persisting. Errors from the DB are returned for
// the caller to handle.
func (r *BookingRepository) Create(ctx context.Context, booking *domain.Booking) error {
	// Ensure the booking has an ID and timestamps set before persisting.
	if booking.ID == "" {
		booking.ID = uuid.New().String()
	}
	booking.CreatedAt = time.Now()
	booking.Status = string(domain.StatusConfirmed)

	// Use a parameterized INSERT to avoid SQL injection and let the driver
	// handle proper value encoding.
	query := `
		INSERT INTO bookings (id, branch_id, service_id, customer_name, email, phone, date, time_slot, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.ExecContext(ctx, query,
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
	)

	if err != nil {
		// Log the error for diagnostics and propagate it to the caller.
		slog.Error("Operation failed", "operation", "create booking in database", "error", err.Error())
		return err
	}

	return nil
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
func (r *BookingRepository) CreateWithCapacityCheck(ctx context.Context, booking *domain.Booking, branchCapacity int) error {
	// Start a transaction for atomic check-then-insert with row locking
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("Operation failed", "operation", "begin transaction for capacity check", "error", err.Error())
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("Operation failed", "operation", "rollback transaction", "error", err.Error())
		}
	}()

	// Serialise concurrent bookings for this branch. The branch row always
	// exists, even when the slot has no bookings yet, so this lock closes the
	// gap that locking booking rows would leave. It is held until commit or
	// rollback.
	var lockedBranchID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM branches WHERE id = $1 FOR UPDATE`, booking.BranchID).Scan(&lockedBranchID)
	if err != nil {
		if err == sql.ErrNoRows || isInvalidUUID(err) {
			return domain.ErrBranchNotFound
		}
		slog.Error("Operation failed", "operation", "lock branch for capacity check", "error", err.Error())
		return err
	}

	// Count under the lock, so no other booking for this branch can be
	// inserted between this count and our insert.
	var currentCount int
	countQuery := `SELECT COUNT(*) FROM bookings WHERE branch_id = $1 AND date = $2 AND time_slot = $3 AND status != $4`
	err = tx.QueryRowContext(ctx, countQuery, booking.BranchID, booking.Date, booking.TimeSlot, string(domain.StatusCancelled)).Scan(&currentCount)
	if err != nil {
		slog.Error("Operation failed", "operation", "check booking count in transaction", "error", err.Error())
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
		INSERT INTO bookings (id, branch_id, service_id, customer_name, email, phone, date, time_slot, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
	)
	if err != nil {
		slog.Error("Operation failed", "operation", "insert booking in transaction", "error", err.Error())
		return err
	}

	// Commit the transaction to release locks and persist changes
	if err := tx.Commit(); err != nil {
		slog.Error("Operation failed", "operation", "commit transaction", "error", err.Error())
		return err
	}

	return nil
}

// GetAll returns all bookings joined with branch and service names. The
// function maps SQL rows into domain.Booking objects and includes related
// branch/service names for convenience in higher layers.
func (r *BookingRepository) GetAll(ctx context.Context) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, b.date, b.time_slot, b.status, b.created_at,
			   br.name as branch_name, s.name as service_name
		FROM bookings b
		JOIN branches br ON b.branch_id = br.id
		JOIN services s ON b.service_id = s.id
		ORDER BY b.date DESC, b.time_slot DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		slog.Error("Operation failed", "operation", "get all bookings from database", "error", err.Error())
		return nil, err
	}
	// Ensure rows are closed to release DB resources back to the pool.
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	var bookings []*domain.Booking
	for rows.Next() {
		booking := &domain.Booking{}
		var branchName, serviceName string
		// The Scan order must match the SELECT column order above.
		err := rows.Scan(
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
		)
		if err != nil {
			slog.Error("Operation failed", "operation", "scan booking row", "error", err.Error())
			return nil, err
		}
		// Populate related models with minimal data (ID + name) for callers.
		booking.Branch = &domain.Branch{ID: booking.BranchID, Name: branchName}
		booking.Service = &domain.Service{ID: booking.ServiceID, Name: serviceName}
		bookings = append(bookings, booking)
	}

	// Check for iteration errors that may have occurred during rows.Next().
	if err = rows.Err(); err != nil {
		slog.Error("Operation failed", "operation", "iterate booking rows", "error", err.Error())
		return nil, err
	}

	return bookings, nil
}

// GetAllPaginated returns bookings with pagination applied.
// limit is the max number of results per page, offset is the start position.
func (r *BookingRepository) GetAllPaginated(ctx context.Context, limit int, offset int) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, b.date, b.time_slot, b.status, b.created_at,
			   br.name as branch_name, s.name as service_name
		FROM bookings b
		JOIN branches br ON b.branch_id = br.id
		JOIN services s ON b.service_id = s.id
		ORDER BY b.date DESC, b.time_slot DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		slog.Error("Operation failed", "operation", "get paginated bookings from database", "error", err.Error())
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	var bookings []*domain.Booking
	for rows.Next() {
		booking := &domain.Booking{}
		var branchName, serviceName string
		err := rows.Scan(
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
		)
		if err != nil {
			slog.Error("Operation failed", "operation", "scan booking row", "error", err.Error())
			return nil, err
		}
		booking.Branch = &domain.Branch{ID: booking.BranchID, Name: branchName}
		booking.Service = &domain.Service{ID: booking.ServiceID, Name: serviceName}
		bookings = append(bookings, booking)
	}

	if err = rows.Err(); err != nil {
		slog.Error("Operation failed", "operation", "iterate booking rows", "error", err.Error())
		return nil, err
	}

	return bookings, nil
}

// GetByEmail returns bookings for a specific customer's email address. It
// uses the same row mapping approach as GetAll but filters by email.
func (r *BookingRepository) GetByEmail(ctx context.Context, email string) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, b.date, b.time_slot, b.status, b.created_at,
			   br.name as branch_name, s.name as service_name
		FROM bookings b
		JOIN branches br ON b.branch_id = br.id
		JOIN services s ON b.service_id = s.id
		WHERE b.email = $1
		ORDER BY b.date DESC, b.time_slot DESC
	`

	rows, err := r.db.QueryContext(ctx, query, email)
	if err != nil {
		slog.Error("Operation failed", "operation", "get bookings by email from database", "error", err.Error())
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	var bookings []*domain.Booking
	for rows.Next() {
		booking := &domain.Booking{}
		var branchName, serviceName string
		err := rows.Scan(
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
		)
		if err != nil {
			slog.Error("Operation failed", "operation", "scan booking row by email", "error", err.Error())
			return nil, err
		}
		booking.Branch = &domain.Branch{ID: booking.BranchID, Name: branchName}
		booking.Service = &domain.Service{ID: booking.ServiceID, Name: serviceName}
		bookings = append(bookings, booking)
	}

	if err = rows.Err(); err != nil {
		slog.Error("Operation failed", "operation", "iterate booking rows by email", "error", err.Error())
		return nil, err
	}

	return bookings, nil
}

// GetByEmailPaginated returns bookings for a specific email with pagination.
// limit is the max number of results per page, offset is the start position.
func (r *BookingRepository) GetByEmailPaginated(ctx context.Context, email string, limit int, offset int) ([]*domain.Booking, error) {
	query := `
		SELECT b.id, b.branch_id, b.service_id, b.customer_name, b.email, b.phone, b.date, b.time_slot, b.status, b.created_at,
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
		slog.Error("Operation failed", "operation", "get paginated bookings by email from database", "error", err.Error())
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	var bookings []*domain.Booking
	for rows.Next() {
		booking := &domain.Booking{}
		var branchName, serviceName string
		err := rows.Scan(
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
		)
		if err != nil {
			slog.Error("Operation failed", "operation", "scan booking row by email", "error", err.Error())
			return nil, err
		}
		booking.Branch = &domain.Branch{ID: booking.BranchID, Name: branchName}
		booking.Service = &domain.Service{ID: booking.ServiceID, Name: serviceName}
		bookings = append(bookings, booking)
	}

	if err = rows.Err(); err != nil {
		slog.Error("Operation failed", "operation", "iterate booking rows by email", "error", err.Error())
		return nil, err
	}

	return bookings, nil
}

// GetBookingCount returns the number of non-cancelled bookings for the
// specified branch/date/timeSlot. This is typically used to enforce capacity
// constraints before creating new bookings.
func (r *BookingRepository) GetBookingCount(ctx context.Context, branchID, date, timeSlot string) (int, error) {
	query := `SELECT COUNT(*) FROM bookings WHERE branch_id = $1 AND date = $2 AND time_slot = $3 AND status != $4`
	var count int
	err := r.db.QueryRowContext(ctx, query, branchID, date, timeSlot, string(domain.StatusCancelled)).Scan(&count)
	if err != nil {
		slog.Error("Operation failed", "operation", "get booking count from database", "error", err.Error())
		return 0, err
	}
	return count, nil
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
		slog.Error("Operation failed", "operation", "get booked slots from database", "error", err.Error())
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close rows", "error", err.Error())
		}
	}()

	slots := make(map[string]int)
	for rows.Next() {
		var timeSlot string
		var count int
		err := rows.Scan(&timeSlot, &count)
		if err != nil {
			slog.Error("Operation failed", "operation", "scan booked slot row", "error", err.Error())
			return nil, err
		}
		slots[timeSlot] = count
	}

	if err = rows.Err(); err != nil {
		slog.Error("Operation failed", "operation", "iterate booked slot rows", "error", err.Error())
		return nil, err
	}

	return slots, nil
}

// Cancel marks the booking status as cancelled. If no rows are affected the
// function returns domain.ErrBookingNotFound so callers can handle the case
// where the booking does not exist.
func (r *BookingRepository) Cancel(ctx context.Context, bookingID string) error {
	query := `UPDATE bookings SET status = $1 WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, string(domain.StatusCancelled), bookingID)
	if err != nil {
		slog.Error("Operation failed", "operation", "cancel booking in database", "error", err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		slog.Error("Operation failed", "operation", "get rows affected for booking cancellation", "error", err.Error())
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBookingNotFound
	}

	return nil
}
