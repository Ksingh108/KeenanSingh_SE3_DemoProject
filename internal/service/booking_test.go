package service

import (
	"context"
	"testing"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
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
		Phone:        "+44 7700 900000",
		Date:         futureDate,
		TimeSlot:     "09:00",
	}

	err := service.CreateBooking(ctx, booking)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestBookingService_GetAllBookings(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	ctx := context.Background()

	bookings, err := service.GetAllBookings(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(bookings) != 2 {
		t.Errorf("Expected 2 bookings, got %d", len(bookings))
	}
}

func TestBookingService_GetBookingsByEmail(t *testing.T) {
	bookingRepo := memory.NewBookingRepository("partial")
	branchRepo := memory.NewBranchRepository()
	service := NewBookingService(bookingRepo, branchRepo)
	ctx := context.Background()

	bookings, err := service.GetBookingsByEmail(ctx, "harry@hogwarts.edu")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(bookings) != 1 {
		t.Errorf("Expected 1 booking, got %d", len(bookings))
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
