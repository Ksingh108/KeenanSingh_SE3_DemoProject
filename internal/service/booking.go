// Package service contains the business logic layer of the application.
// Services orchestrate operations between the domain and repository layers,
// implementing business rules, validation, and complex operations.
package service

import (
	"context"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository"
)

// BookingService handles all booking-related business logic and operations.
// It coordinates between booking and branch repositories to ensure business rules
// are enforced, such as capacity limits and availability checking.
type BookingService struct {
	bookingRepo repository.BookingRepository // Repository for booking data operations
	branchRepo  repository.BranchRepository  // Repository for branch data operations
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
	return &BookingService{
		bookingRepo: bookingRepo,
		branchRepo:  branchRepo,
	}
}

// CreateBooking creates a new booking after validating business rules.
// It ensures that the branch exists, has capacity for the requested time slot,
// and that the booking doesn't exceed the branch's capacity limits.
//
// Business Rules Enforced:
//   - Branch must exist and be valid
//   - Time slot must not exceed branch capacity
//   - Booking data must be valid (validated at handler level)
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
//   - error: nil on success, domain.ErrCapacityExceeded if capacity exceeded,
//     domain.ErrBranchNotFound if branch doesn't exist, or other repository errors
func (s *BookingService) CreateBooking(ctx context.Context, booking *domain.Booking) error {
	// Get branch to check capacity and validate existence.
	branch, err := s.branchRepo.GetByID(ctx, booking.BranchID)
	if err != nil {
		return err // Could be ErrBranchNotFound or database error
	}

	// Capacity is enforced as part of the create operation so concurrent requests
	// cannot exceed the branch limit during the check/insert race window.
	return s.bookingRepo.CreateWithCapacityCheck(ctx, booking, branch.Capacity)
}

// GetAllBookings retrieves all bookings from the system.
// This method is typically used for administrative purposes or reporting.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//
// Returns:
//   - []*domain.Booking: Slice of all bookings in the system
//   - error: nil on success, or repository error on failure
func (s *BookingService) GetAllBookings(ctx context.Context) ([]*domain.Booking, error) {
	return s.bookingRepo.GetAll(ctx)
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

// GetBookingsByEmail retrieves all bookings for a specific customer email.
// This allows customers to view their booking history and current appointments.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - email: Customer's email address (should be pre-validated)
//
// Returns:
//   - []*domain.Booking: Slice of bookings for the specified email
//   - error: nil on success, or repository error on failure
func (s *BookingService) GetBookingsByEmail(ctx context.Context, email string) ([]*domain.Booking, error) {
	return s.bookingRepo.GetByEmail(ctx, email)
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

	// Define all possible time slots
	allSlots := []string{
		"09:00", "09:30", "10:00", "10:30", "11:00", "11:30",
		"12:00", "12:30", "13:00", "13:30", "14:00", "14:30",
		"15:00", "15:30", "16:00", "16:30", "17:00",
	}

	// Get booking counts for all slots in a single query
	bookedSlots, err := s.bookingRepo.GetBookedSlots(ctx, branchID, date)
	if err != nil {
		return nil, err
	}

	// Build availability map: mark as unavailable if at or over capacity
	result := make(map[string]bool)
	for _, slot := range allSlots {
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
