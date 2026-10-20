// Package service contains the business logic layer of the application.
// Services orchestrate operations between the domain and repository layers,
// implementing business rules, validation, and complex operations.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository"
	"github.com/gringottsbankbranchbooking/pkg/clock"
	"github.com/gringottsbankbranchbooking/pkg/validator"
)

// BookingValidationError identifies input rejected by booking business rules.
type BookingValidationError struct {
	err error
}

func (e *BookingValidationError) Error() string {
	return e.err.Error()
}

func (e *BookingValidationError) Unwrap() error {
	return e.err
}

// BookingService handles all booking-related business logic and operations.
// It coordinates between booking and branch repositories to ensure business rules
// are enforced, such as capacity limits and availability checking.
type BookingService struct {
	bookingRepo repository.BookingRepository // Repository for booking data operations
	branchRepo  repository.BranchRepository  // Repository for branch data operations
	clock       clock.Clock                  // Source of "now" for time-sensitive business validation
}

// NewBookingService creates a new instance of BookingService with the required dependencies.
// It takes booking and branch repositories to handle data operations and business logic.
//
// Parameters:
//   - bookingRepo: Repository interface for booking data operations
//   - branchRepo: Repository interface for branch data operations
//
// Returns:
//   - *BookingService: Configured service instance ready for use
func NewBookingService(bookingRepo repository.BookingRepository, branchRepo repository.BranchRepository) *BookingService {
	return NewBookingServiceWithClock(bookingRepo, branchRepo, clock.NewBusinessClock())
}

// NewBookingServiceWithClock creates a BookingService using the supplied clock,
// which is useful for testing time-sensitive booking rules deterministically.
func NewBookingServiceWithClock(bookingRepo repository.BookingRepository, branchRepo repository.BranchRepository, c clock.Clock) *BookingService {
	return &BookingService{
		bookingRepo: bookingRepo,
		branchRepo:  branchRepo,
		clock:       c,
	}
}

// SetClock overrides the service's notion of "now" for time-sensitive validation.
func (s *BookingService) SetClock(c clock.Clock) {
	if c != nil {
		s.clock = c
	}
}

// CreateBooking creates a new booking after validating business rules.
// It ensures that the branch exists, offers the requested service, and has
// capacity for the requested time slot.
//
// Business Rules Enforced:
//   - Branch must exist and be valid
//   - Requested service must be offered at the branch
//   - Time slot must have available branch capacity
//   - Booking data must be valid
//
// Concurrency Safety:
//   - Uses atomic capacity check and booking creation (the repository locks the branch row in PostgreSQL)
//   - Prevents double-booking from concurrent requests
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - booking: Booking entity to be created
//
// Returns:
//   - error: nil on success, domain.ErrServiceNotAvailableAtBranch if the branch
//     doesn't offer the requested service, domain.ErrCapacityExceeded if capacity
//     is exceeded, domain.ErrBranchNotFound if the branch doesn't exist, or another error
func (s *BookingService) CreateBooking(ctx context.Context, booking *domain.Booking) error {
	return s.createBooking(ctx, booking, "")
}

// CreateBookingIdempotent creates a booking once for a key and replays that
// booking for subsequent requests with the same normalized request data.
func (s *BookingService) CreateBookingIdempotent(ctx context.Context, booking *domain.Booking, key string) error {
	return s.createBooking(ctx, booking, key)
}

func (s *BookingService) createBooking(ctx context.Context, booking *domain.Booking, idempotencyKey string) error {
	fingerprint := ""
	if idempotencyKey != "" {
		request, err := json.Marshal(struct {
			BranchID     string `json:"branch_id"`
			ServiceID    string `json:"service_id"`
			CustomerName string `json:"customer_name"`
			Email        string `json:"email"`
			Phone        string `json:"phone"`
			Date         string `json:"date"`
			TimeSlot     string `json:"time_slot"`
		}{booking.BranchID, booking.ServiceID, booking.CustomerName, booking.Email, validator.NormalizePhone(booking.Phone), booking.Date, booking.TimeSlot})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(request)
		fingerprint = hex.EncodeToString(digest[:])
		existing, existingFingerprint, found, err := s.bookingRepo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existingFingerprint != fingerprint {
				return domain.ErrIdempotencyKeyReused
			}
			if existing.Status == string(domain.StatusCancelled) {
				return domain.ErrIdempotentBookingCancelled
			}
			*booking = *existing
			return nil
		}
	}

	if err := s.ValidateBooking(booking, s.clock.Now()); err != nil {
		return &BookingValidationError{err: err}
	}

	// Get branch to check capacity and validate existence.
	_, err := s.branchRepo.GetByID(ctx, booking.BranchID)
	if err != nil {
		return err // Could be ErrBranchNotFound or database error
	}
	services, err := s.branchRepo.GetServices(ctx, booking.BranchID)
	if err != nil {
		return err
	}
	serviceAvailable := false
	for _, availableService := range services {
		if availableService.ID == booking.ServiceID {
			serviceAvailable = true
			break
		}
	}
	if !serviceAvailable {
		return domain.ErrServiceNotAvailableAtBranch
	}

	// Capacity is enforced as part of the create operation so concurrent requests
	// cannot exceed the branch limit during the check/insert race window.
	if idempotencyKey == "" {
		return s.bookingRepo.CreateWithCapacityCheck(ctx, booking)
	}
	return s.bookingRepo.CreateWithCapacityCheckIdempotent(ctx, booking, idempotencyKey, fingerprint)
}

// ValidateBooking enforces the core domain invariants for a booking before the
// repository is asked to persist or look up data. This keeps HTTP parsing and
// response formatting in the handler while the business rules live at the service
// boundary, where non-HTTP callers also benefit from the same checks.
func (s *BookingService) ValidateBooking(booking *domain.Booking, now time.Time) error {
	if booking == nil {
		return fmt.Errorf("booking is required")
	}
	if !validator.ValidateEmail(booking.Email) {
		return fmt.Errorf("invalid email format")
	}
	if !validator.ValidatePhone(booking.Phone) {
		return fmt.Errorf("invalid phone format: use a South African number, for example +27 82 123 4567 or 082 123 4567")
	}
	booking.Phone = validator.NormalizePhone(booking.Phone)
	if !validator.ValidateCustomerName(booking.CustomerName) {
		return fmt.Errorf("invalid customer name format")
	}
	if !validator.ValidateServiceID(booking.ServiceID) {
		return fmt.Errorf("invalid service ID format")
	}
	if !validator.ValidateDate(booking.Date, now) {
		return fmt.Errorf("invalid date format or date in the past")
	}
	if !validator.ValidateTimeSlot(booking.TimeSlot) {
		return fmt.Errorf("invalid time slot format")
	}
	if !validator.IsBookableTimeSlot(booking.TimeSlot) {
		return fmt.Errorf("time slot is not on the availability grid")
	}
	if !validator.ValidateSlotNotPast(booking.Date, booking.TimeSlot, now) {
		return domain.ErrBookingSlotInPast
	}
	if !validator.ValidateBranchID(booking.BranchID) {
		return fmt.Errorf("invalid branch ID")
	}
	return nil
}

// GetAllBookingsPaginated retrieves bookings from the system with pagination.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - limit: Maximum number of results per page (1-1000)
//   - offset: Number of results to skip
//
// Returns:
//   - []*domain.Booking: Slice of paginated bookings
//   - error: nil on success, or repository error on failure
func (s *BookingService) GetAllBookingsPaginated(ctx context.Context, limit int, offset int) ([]*domain.Booking, error) {
	return s.bookingRepo.GetAllPaginated(ctx, limit, offset)
}

// GetBookingsByEmailPaginated retrieves bookings for a customer with pagination.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - email: Customer's email address (should be pre-validated)
//   - limit: Maximum number of results per page (1-1000)
//   - offset: Number of results to skip
//
// Returns:
//   - []*domain.Booking: Slice of paginated bookings for the email
//   - error: nil on success, or repository error on failure
func (s *BookingService) GetBookingsByEmailPaginated(ctx context.Context, email string, limit int, offset int) ([]*domain.Booking, error) {
	return s.bookingRepo.GetByEmailPaginated(ctx, email, limit, offset)
}

// CheckAvailability checks which time slots are available for booking at a specific branch and date.
// Returns a map indicating which time slots are at capacity (true = at capacity, false = available).
// This method considers branch capacity limits, not just whether any booking exists.
//
// Uses a single batch query to fetch all booked slots instead of querying each slot individually,
// reducing database queries from 17 to 1.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - branchID: ID of the branch to check availability for
//   - date: Date to check in YYYY-MM-DD format
//
// Returns:
//   - map[string]bool: Map of time slots to capacity status (true = at capacity, false = available)
//   - error: nil on success, or repository error on failure
func (s *BookingService) CheckAvailability(ctx context.Context, branchID, date string) (map[string]bool, error) {
	// Get branch to check capacity
	branch, err := s.branchRepo.GetByID(ctx, branchID)
	if err != nil {
		return nil, err
	}

	// Get booking counts for all slots in a single query
	bookedSlots, err := s.bookingRepo.GetBookedSlots(ctx, branchID, date)
	if err != nil {
		return nil, err
	}

	// Build availability map: mark as unavailable if at or over capacity
	result := make(map[string]bool)
	for _, slot := range validator.BookableTimeSlots() {
		count := bookedSlots[slot]
		result[slot] = count >= branch.Capacity
	}

	return result, nil
}

// CancelBooking cancels an existing booking by marking it as cancelled.
// This method is typically used by admin users to cancel bookings on behalf of customers.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - bookingID: ID of the booking to cancel
//
// Returns:
//   - error: nil on success, domain.ErrBookingNotFound if booking doesn't exist,
//     or repository error on failure
func (s *BookingService) CancelBooking(ctx context.Context, bookingID string) error {
	return s.bookingRepo.Cancel(ctx, bookingID)
}
