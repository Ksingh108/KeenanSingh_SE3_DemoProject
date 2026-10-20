package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
	"github.com/gringottsbankbranchbooking/pkg/clock"
)

func TestBookingService_CreateBooking(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	ctx := context.Background()
	futureDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")

	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         futureDate,
		TimeSlot:     "09:00",
	}

	err := service.CreateBooking(ctx, booking)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestBookingService_CreateBookingIdempotent_ReplaysAfterSlotIsPast(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	service := NewBookingServiceWithClock(bookingRepo, memory.NewBranchRepository(), clock.Fixed(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))
	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         "2030-01-10",
		TimeSlot:     "09:00",
	}
	if err := service.CreateBookingIdempotent(context.Background(), booking, "retry-key"); err != nil {
		t.Fatalf("create booking: %v", err)
	}
	createdID := booking.ID
	service.SetClock(clock.Fixed(time.Date(2030, 1, 11, 0, 0, 0, 0, time.UTC)))

	retry := &domain.Booking{
		BranchID: booking.BranchID, ServiceID: booking.ServiceID,
		CustomerName: booking.CustomerName, Email: booking.Email, Phone: "+27 82 123 4567",
		Date: booking.Date, TimeSlot: booking.TimeSlot,
	}
	if err := service.CreateBookingIdempotent(context.Background(), retry, "retry-key"); err != nil {
		t.Fatalf("replay booking after slot became past: %v", err)
	}
	if retry.ID != createdID {
		t.Fatalf("replayed booking ID = %q, want %q", retry.ID, createdID)
	}
}

func TestBookingService_CreateBooking_BranchNotFound(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	booking := &domain.Booking{
		BranchID:     "770e8400-e29b-41d4-a716-4466554400ff",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "09:00",
	}

	err := service.CreateBooking(context.Background(), booking)
	if !errors.Is(err, domain.ErrBranchNotFound) {
		t.Fatalf("Expected ErrBranchNotFound, got %v", err)
	}

	bookings, err := bookingRepo.GetAllPaginated(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("Failed to retrieve bookings: %v", err)
	}
	if len(bookings) != 0 {
		t.Errorf("Expected no booking to be persisted, got %d", len(bookings))
	}
}

func TestBookingService_CreateBooking_ServiceNotAvailableAtBranch(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440002",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "09:00",
	}

	err := service.CreateBooking(context.Background(), booking)
	if !errors.Is(err, domain.ErrServiceNotAvailableAtBranch) {
		t.Fatalf("Expected ErrServiceNotAvailableAtBranch, got %v", err)
	}

	bookings, err := bookingRepo.GetAllPaginated(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("Failed to retrieve bookings: %v", err)
	}
	if len(bookings) != 0 {
		t.Errorf("Expected no booking to be persisted, got %d", len(bookings))
	}
}

func TestBookingService_CreateBooking_RejectsInvalidPhoneBeforeRepo(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "abc",
		Date:         time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		TimeSlot:     "09:00",
	}

	err := service.CreateBooking(context.Background(), booking)
	if err == nil {
		t.Fatal("expected invalid phone to be rejected by service validation")
	}

	bookings, err := bookingRepo.GetAllPaginated(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("Failed to retrieve bookings: %v", err)
	}
	if len(bookings) != 0 {
		t.Errorf("Expected no booking to be persisted, got %d", len(bookings))
	}
}

func TestBookingService_CheckAvailability(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	ctx := context.Background()
	futureDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")

	// CheckAvailability returns a map of all 17 time slots with their availability status.
	// true = unavailable (at or over capacity), false = available
	availability, err := service.CheckAvailability(ctx, "550e8400-e29b-41d4-a716-446655440001", futureDate)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Expect all 17 slots in the response (9:00-17:00 in 30-min intervals)
	if len(availability) != 17 {
		t.Errorf("Expected 17 time slots, got %d", len(availability))
	}

	// Verify that most slots are available (false) since capacity is 15
	// The "partial" data has 2 bookings, both well under capacity
	if availability["09:00"] || availability["10:00"] {
		t.Error("Booked slots should still be available (under capacity)")
	}

	// Verify that an unboooked slot is available
	if availability["11:00"] {
		t.Error("Expected 11:00 slot to be available")
	}
}

func TestBookingService_CreateBookingIdempotent_CancelledBookingIsNotReplayed(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	service := NewBookingServiceWithClock(bookingRepo, memory.NewBranchRepository(), clock.Fixed(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))
	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         "2030-01-10",
		TimeSlot:     "09:00",
	}
	if err := service.CreateBookingIdempotent(context.Background(), booking, "cancel-key"); err != nil {
		t.Fatalf("create booking: %v", err)
	}
	if err := bookingRepo.Cancel(context.Background(), booking.ID); err != nil {
		t.Fatalf("cancel booking: %v", err)
	}

	retry := *booking
	retry.ID = ""
	err := service.CreateBookingIdempotent(context.Background(), &retry, "cancel-key")
	if !errors.Is(err, domain.ErrIdempotentBookingCancelled) {
		t.Fatalf("retry after cancellation error = %v, want ErrIdempotentBookingCancelled", err)
	}
}

func TestBookingService_CreateBookingIdempotent_ReplaysAcrossPhoneFormats(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("empty")
	service := NewBookingServiceWithClock(bookingRepo, memory.NewBranchRepository(), clock.Fixed(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))
	booking := &domain.Booking{
		BranchID:     "550e8400-e29b-41d4-a716-446655440001",
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Test Customer",
		Email:        "test@example.com",
		Phone:        "+27 82 123 4567",
		Date:         "2030-01-10",
		TimeSlot:     "09:00",
	}
	if err := service.CreateBookingIdempotent(context.Background(), booking, "phone-key"); err != nil {
		t.Fatalf("create booking: %v", err)
	}

	retry := &domain.Booking{
		BranchID: booking.BranchID, ServiceID: booking.ServiceID,
		CustomerName: booking.CustomerName, Email: booking.Email, Phone: "082 123 4567",
		Date: booking.Date, TimeSlot: booking.TimeSlot,
	}
	if err := service.CreateBookingIdempotent(context.Background(), retry, "phone-key"); err != nil {
		t.Fatalf("replay with a differently formatted phone: %v", err)
	}
	if retry.ID != booking.ID {
		t.Fatalf("replayed booking ID = %q, want %q", retry.ID, booking.ID)
	}
}
