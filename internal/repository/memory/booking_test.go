package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

const (
	testBranchID = "550e8400-e29b-41d4-a716-446655440001"
	testDate     = "2030-01-01"
	testSlot     = "09:00"
)

func newSlotBooking() *domain.Booking {
	return &domain.Booking{
		BranchID:     testBranchID,
		ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
		CustomerName: "Jane Smith",
		Email:        "jane@example.com",
		Phone:        "+27 21 555 0100",
		Date:         testDate,
		TimeSlot:     testSlot,
	}
}

// runConcurrentBookings fires attempts simultaneous CreateWithCapacityCheck
// calls for the same empty slot and reports how many succeeded and how many
// were rejected for capacity. Any other error fails the test.
func runConcurrentBookings(t *testing.T, repo *BookingRepository, attempts int) (succeeded, rejected int) {
	t.Helper()

	var succeededCount, rejectedCount atomic.Int32
	var ready, done sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start // release every goroutine together to maximise contention

			err := repo.CreateWithCapacityCheck(context.Background(), newSlotBooking())
			switch {
			case err == nil:
				succeededCount.Add(1)
			case errors.Is(err, domain.ErrCapacityExceeded):
				rejectedCount.Add(1)
			default:
				t.Errorf("Unexpected error: %v", err)
			}
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()
	return int(succeededCount.Load()), int(rejectedCount.Load())
}

// The core safety claim: however many requests race for the last places in a
// slot, exactly `capacity` of them win and the slot is never overbooked.
func TestBookingRepository_CreateWithCapacityCheck_ConcurrentNeverOverbooks(t *testing.T) {
	const capacity = 5
	const attempts = 100
	repo := NewBookingRepository("empty", capacity)

	succeeded, rejected := runConcurrentBookings(t, repo, attempts)

	if succeeded != capacity {
		t.Errorf("Expected exactly %d bookings to succeed, got %d", capacity, succeeded)
	}
	if rejected != attempts-capacity {
		t.Errorf("Expected %d rejections, got %d", attempts-capacity, rejected)
	}

	storedSlots, err := repo.GetBookedSlots(context.Background(), testBranchID, testDate)
	if err != nil {
		t.Fatalf("GetBookedSlots failed: %v", err)
	}
	if storedSlots[testSlot] != capacity {
		t.Errorf("Expected %d stored bookings, got %d", capacity, storedSlots[testSlot])
	}
}

func TestBookingRepository_CreateWithCapacityCheck_CapacityOfOneAllowsExactlyOne(t *testing.T) {
	repo := NewBookingRepository("empty", 1)

	succeeded, rejected := runConcurrentBookings(t, repo, 50)

	if succeeded != 1 || rejected != 49 {
		t.Errorf("Expected 1 success and 49 rejections, got %d and %d", succeeded, rejected)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_CancelledBookingFreesCapacity(t *testing.T) {
	ctx := context.Background()
	repo := NewBookingRepository("empty", 1)

	first := newSlotBooking()
	if err := repo.CreateWithCapacityCheck(ctx, first); err != nil {
		t.Fatalf("First booking failed: %v", err)
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); !errors.Is(err, domain.ErrCapacityExceeded) {
		t.Fatalf("Expected ErrCapacityExceeded for a full slot, got %v", err)
	}

	if err := repo.Cancel(ctx, first.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); err != nil {
		t.Errorf("Expected the cancelled booking's place to be reusable, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_DifferentSlotsAreIndependent(t *testing.T) {
	ctx := context.Background()
	repo := NewBookingRepository("empty", 1)

	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); err != nil {
		t.Fatalf("Booking failed: %v", err)
	}

	otherSlot := newSlotBooking()
	otherSlot.TimeSlot = "10:00"
	if err := repo.CreateWithCapacityCheck(ctx, otherSlot); err != nil {
		t.Errorf("A full 09:00 slot must not block 10:00, got %v", err)
	}
}
