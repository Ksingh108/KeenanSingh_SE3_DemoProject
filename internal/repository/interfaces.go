// Package repository defines the data access layer interfaces for the application.
// These interfaces abstract the data storage implementation, allowing for easy
// testing with mock implementations and flexibility to change storage backends.
//
// All repository methods use context.Context for cancellation and timeout control.
package repository

import (
	"context"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// BookingRepository defines the interface for booking data operations.
// Implementations handle persistence and retrieval of booking information,
// including availability checking and capacity management.
type BookingRepository interface {
	// Create persists a new booking to the data store
	Create(ctx context.Context, booking *domain.Booking) error

	// CreateWithCapacityCheck atomically checks capacity and creates a booking in a single transaction.
	// This prevents double-booking by ensuring the capacity check and insert happen without race conditions.
	// PostgreSQL locks the branch row (SELECT...FOR UPDATE) for the transaction, which works even
	// when the slot has no bookings yet; the in-memory implementation uses a mutex.
	// Concurrent behaviour is verified by the repository tests, not just asserted here.
	//
	// Returns domain.ErrCapacityExceeded if the time slot is at or over capacity
	CreateWithCapacityCheck(ctx context.Context, booking *domain.Booking, branchCapacity int) error

	// GetAll retrieves all bookings from the data store
	GetAll(ctx context.Context) ([]*domain.Booking, error)

	// GetAllPaginated retrieves bookings from the data store with pagination.
	// limit specifies max results per page (1-1000), offset specifies start position
	GetAllPaginated(ctx context.Context, limit int, offset int) ([]*domain.Booking, error)

	// GetByEmail retrieves all bookings for a specific customer email
	GetByEmail(ctx context.Context, email string) ([]*domain.Booking, error)

	// GetByEmailPaginated retrieves bookings for a specific email with pagination.
	// limit specifies max results per page (1-1000), offset specifies start position
	GetByEmailPaginated(ctx context.Context, email string, limit int, offset int) ([]*domain.Booking, error)

	// GetBookingCount returns the number of bookings for a specific branch, date, and time slot
	// Used for capacity checking before creating new bookings
	GetBookingCount(ctx context.Context, branchID, date, timeSlot string) (int, error)

	// GetBookedSlots returns a map of all booked time slots for a specific branch and date.
	// The map contains {timeSlot: count} for all slots with bookings.
	// This is more efficient than calling GetBookingCount for each slot individually.
	GetBookedSlots(ctx context.Context, branchID, date string) (map[string]int, error)

	// Cancel marks a booking as cancelled by its ID
	// Returns domain.ErrBookingNotFound if booking doesn't exist
	Cancel(ctx context.Context, bookingID string) error
}

// BranchRepository defines the interface for branch data operations.
// Handles branch information, locations, capacity, and associated services.
type BranchRepository interface {
	// GetAll retrieves all branch locations
	GetAll(ctx context.Context) ([]*domain.Branch, error)

	// GetByID retrieves a specific branch by its ID
	// Returns domain.ErrBranchNotFound if branch doesn't exist
	GetByID(ctx context.Context, id string) (*domain.Branch, error)

	// GetServices retrieves all services available at a specific branch
	GetServices(ctx context.Context, branchID string) ([]*domain.Service, error)

	// Create adds a new branch with associated services to the data store
	Create(ctx context.Context, branch *domain.Branch, serviceIDs []string) (*domain.Branch, error)

	// Delete removes a branch from the data store and returns the number of
	// bookings that were removed along with it (bookings reference branches
	// with ON DELETE CASCADE, so they cannot outlive their branch in PostgreSQL).
	// Returns domain.ErrBranchNotFound if branch doesn't exist
	Delete(ctx context.Context, id string) (bookingsRemoved int, err error)
}

// AuditRepository defines the interface for the admin audit trail.
// Entries are append-only: the application never updates or deletes them.
type AuditRepository interface {
	// Create appends an audit entry. CreatedAt is set by the data store.
	Create(ctx context.Context, entry *domain.AuditEntry) error
}

// ServiceRepository defines the interface for service data operations.
// Manages banking services that can be booked at branches.
type ServiceRepository interface {
	// GetAll retrieves all available banking services
	GetAll(ctx context.Context) ([]*domain.Service, error)
}

// AdminRepository defines the interface for admin user data operations.
// Handles authentication and admin user management.
type AdminRepository interface {
	// GetByUsername retrieves admin user information by username
	// Used for authentication during login process
	GetByUsername(ctx context.Context, username string) (*domain.Admin, error)
}

// SessionRepository defines the interface for admin session management.
// Handles session creation, validation, and cleanup for admin authentication.
type SessionRepository interface {
	// Create persists a new admin session. Sessions are keyed by token hash;
	// the raw bearer token never reaches the repository.
	Create(ctx context.Context, session *domain.AdminSession) error

	// GetByTokenHash retrieves a session by the hash of its token for validation
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.AdminSession, error)

	// Delete removes a session by token hash (used for logout)
	Delete(ctx context.Context, tokenHash string) error

	// CleanupExpired removes all expired sessions from the data store
	// Called periodically to prevent session table growth
	CleanupExpired(ctx context.Context) error
}
