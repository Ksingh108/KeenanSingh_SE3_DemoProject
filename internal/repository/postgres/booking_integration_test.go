//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

// runConcurrentBookings fires `attempts` simultaneous CreateWithCapacityCheck
// calls for the same slot and reports how many succeeded and how many were
// rejected for capacity. Any other error fails the test.
func runConcurrentBookings(t *testing.T, repo *BookingRepository, attempts, capacity int) (succeeded, rejected int) {
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

			err := repo.CreateWithCapacityCheck(context.Background(), newSlotBooking(), capacity)
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

// This is the proof behind the "no double-booking" claim. The slot starts
// empty, which is the hard case: a lock on existing booking rows protects
// nothing when there are no rows yet, so concurrent first bookings could all
// pass the capacity check and overbook the slot.
func TestBookingRepository_CreateWithCapacityCheck_ConcurrentNeverOverbooks(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)
	const capacity = 5
	const attempts = 40

	succeeded, rejected := runConcurrentBookings(t, repo, attempts, capacity)

	if succeeded != capacity {
		t.Errorf("Expected exactly %d bookings to succeed, got %d", capacity, succeeded)
	}
	if rejected != attempts-capacity {
		t.Errorf("Expected %d rejections, got %d", attempts-capacity, rejected)
	}
	if stored := countBookings(t, seededBranchID, testSlotDate, testSlotTimeSlot); stored != capacity {
		t.Errorf("Expected %d stored bookings, got %d (the slot was overbooked)", capacity, stored)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_CapacityOfOneAllowsExactlyOne(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)

	succeeded, rejected := runConcurrentBookings(t, repo, 30, 1)

	if succeeded != 1 || rejected != 29 {
		t.Errorf("Expected 1 success and 29 rejections, got %d and %d", succeeded, rejected)
	}
	if stored := countBookings(t, seededBranchID, testSlotDate, testSlotTimeSlot); stored != 1 {
		t.Errorf("Expected 1 stored booking, got %d", stored)
	}
}

// Locking is per branch, so contention on one branch must not leak into
// another: each branch fills to its own capacity independently.
func TestBookingRepository_CreateWithCapacityCheck_BranchesAreIndependent(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)
	const capacity = 3

	var wg sync.WaitGroup
	var succeededA, succeededB atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		for _, branch := range []struct {
			id      string
			counter *atomic.Int32
		}{{seededBranchID, &succeededA}, {seededBranchID2, &succeededB}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				booking := newSlotBooking()
				booking.BranchID = branch.id
				if err := repo.CreateWithCapacityCheck(context.Background(), booking, capacity); err == nil {
					branch.counter.Add(1)
				}
			}()
		}
	}
	close(start)
	wg.Wait()

	if succeededA.Load() != capacity || succeededB.Load() != capacity {
		t.Errorf("Expected %d successes per branch, got %d and %d", capacity, succeededA.Load(), succeededB.Load())
	}
}

func TestBookingRepository_CreateWithCapacityCheck_SequentialCapacityIsEnforced(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)

	for i := 0; i < 2; i++ {
		if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking(), 2); err != nil {
			t.Fatalf("Booking %d failed: %v", i+1, err)
		}
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking(), 2); !errors.Is(err, domain.ErrCapacityExceeded) {
		t.Errorf("Expected ErrCapacityExceeded for a full slot, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_CancelledBookingFreesCapacity(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)

	first := newSlotBooking()
	if err := repo.CreateWithCapacityCheck(ctx, first, 1); err != nil {
		t.Fatalf("First booking failed: %v", err)
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking(), 1); !errors.Is(err, domain.ErrCapacityExceeded) {
		t.Fatalf("Expected ErrCapacityExceeded for a full slot, got %v", err)
	}

	if err := repo.Cancel(ctx, first.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking(), 1); err != nil {
		t.Errorf("Expected the cancelled booking's place to be reusable, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_DifferentSlotsAreIndependent(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)

	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking(), 1); err != nil {
		t.Fatalf("Booking failed: %v", err)
	}

	otherSlot := newSlotBooking()
	otherSlot.TimeSlot = "10:00"
	if err := repo.CreateWithCapacityCheck(ctx, otherSlot, 1); err != nil {
		t.Errorf("A full 09:00 slot must not block 10:00, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_UnknownBranch(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)

	booking := newSlotBooking()
	booking.BranchID = "00000000-0000-0000-0000-000000000000"

	err := repo.CreateWithCapacityCheck(context.Background(), booking, 5)
	if !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected ErrBranchNotFound, got %v", err)
	}
}

func TestBookingRepository_CreateAndQuery(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)

	booking := newSlotBooking()
	if err := repo.CreateWithCapacityCheck(ctx, booking, 5); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	count, err := repo.GetBookingCount(ctx, seededBranchID, testSlotDate, testSlotTimeSlot)
	if err != nil || count != 1 {
		t.Errorf("GetBookingCount = %d, %v; want 1, nil", count, err)
	}

	slots, err := repo.GetBookedSlots(ctx, seededBranchID, testSlotDate)
	if err != nil || slots[testSlotTimeSlot] != 1 {
		t.Errorf("GetBookedSlots = %v, %v; want {%s: 1}", slots, err, testSlotTimeSlot)
	}

	byEmail, err := repo.GetByEmail(ctx, "jane@example.com")
	if err != nil || len(byEmail) != 1 || byEmail[0].ID != booking.ID {
		t.Errorf("GetByEmail = %v, %v; want the created booking", byEmail, err)
	}

	if err := repo.Cancel(ctx, booking.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if count, _ := repo.GetBookingCount(ctx, seededBranchID, testSlotDate, testSlotTimeSlot); count != 0 {
		t.Errorf("Expected cancelled booking not to be counted, got %d", count)
	}
}

func TestBookingRepository_Cancel_NotFound(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)

	err := repo.Cancel(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrBookingNotFound) {
		t.Errorf("Expected ErrBookingNotFound, got %v", err)
	}
}
