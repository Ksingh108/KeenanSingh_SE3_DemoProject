//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

// runConcurrentBookings fires `attempts` simultaneous CreateWithCapacityCheck
// calls for the same slot and reports how many succeeded and how many were
// rejected for capacity. Any other error fails the test.
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

// BenchmarkBookingBranchLockContention compares different slots on one branch
// with the same workload split across two branches. Run with Docker available:
// go test -tags integration -run '^$' -bench '^BenchmarkBookingBranchLockContention$' -benchmem -cpu=2,4,8 ./internal/repository/postgres
func BenchmarkBookingBranchLockContention(b *testing.B) {
	for _, scenario := range []struct {
		name          string
		splitBranches bool
	}{
		{name: "same_branch_different_slots"},
		{name: "different_branches_different_slots", splitBranches: true},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			resetDB(b)
			setBranchCapacity(b, seededBranchID, b.N+1)
			setBranchCapacity(b, seededBranchID2, b.N+1)
			repo := NewBookingRepository(testDB)
			var workerCounter atomic.Uint64

			b.ResetTimer()
			b.RunParallel(func(parallel *testing.PB) {
				worker := workerCounter.Add(1) - 1
				branchID, serviceID := seededBranchID, seededServiceID
				if scenario.splitBranches && worker%2 == 1 {
					branchID, serviceID = seededBranchID2, seededServiceID2
				}
				slotOffset := worker
				for parallel.Next() {
					booking := newSlotBooking()
					booking.BranchID = branchID
					booking.ServiceID = serviceID
					if slotOffset%2 == 1 {
						booking.TimeSlot = "10:00"
					}
					if err := repo.CreateWithCapacityCheck(context.Background(), booking); err != nil {
						b.Errorf("create booking: %v", err)
						return
					}
					slotOffset++
				}
			})
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "bookings/s")
		})
	}
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
	setBranchCapacity(t, seededBranchID, capacity)

	succeeded, rejected := runConcurrentBookings(t, repo, attempts)

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
	setBranchCapacity(t, seededBranchID, 1)

	succeeded, rejected := runConcurrentBookings(t, repo, 30)

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
	setBranchCapacity(t, seededBranchID, capacity)
	setBranchCapacity(t, seededBranchID2, capacity)

	var wg sync.WaitGroup
	var succeededA, succeededB atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		for _, branch := range []struct {
			id        string
			serviceID string
			counter   *atomic.Int32
		}{{seededBranchID, seededServiceID, &succeededA}, {seededBranchID2, seededServiceID2, &succeededB}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				booking := newSlotBooking()
				booking.BranchID = branch.id
				booking.ServiceID = branch.serviceID
				if err := repo.CreateWithCapacityCheck(context.Background(), booking); err == nil {
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
	setBranchCapacity(t, seededBranchID, 2)

	for i := 0; i < 2; i++ {
		if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); err != nil {
			t.Fatalf("Booking %d failed: %v", i+1, err)
		}
	}
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); !errors.Is(err, domain.ErrCapacityExceeded) {
		t.Errorf("Expected ErrCapacityExceeded for a full slot, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheckIdempotent_ReplaysAndRejectsMismatch(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)
	const attempts = 20
	var wg sync.WaitGroup
	var idMu sync.Mutex
	var bookingID string
	start := make(chan struct{})
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			booking := newSlotBooking()
			if err := repo.CreateWithCapacityCheckIdempotent(ctx, booking, "integration-retry", "fingerprint-1"); err != nil {
				t.Errorf("create or replay booking: %v", err)
				return
			}
			idMu.Lock()
			defer idMu.Unlock()
			if bookingID == "" {
				bookingID = booking.ID
			} else if booking.ID != bookingID {
				t.Errorf("replayed ID = %q, want %q", booking.ID, bookingID)
			}
		}()
	}
	close(start)
	wg.Wait()
	if bookingID == "" {
		t.Fatal("no booking was created")
	}
	if count := countBookings(t, seededBranchID, testSlotDate, testSlotTimeSlot); count != 1 {
		t.Fatalf("persisted bookings = %d, want 1", count)
	}

	if err := repo.CreateWithCapacityCheckIdempotent(ctx, newSlotBooking(), "integration-retry", "fingerprint-2"); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("mismatched key reuse error = %v, want ErrIdempotencyKeyReused", err)
	}

	// A replayed booking must report its date as YYYY-MM-DD, like the original 201.
	replay := newSlotBooking()
	if err := repo.CreateWithCapacityCheckIdempotent(ctx, replay, "integration-retry", "fingerprint-1"); err != nil {
		t.Fatalf("replay booking: %v", err)
	}
	if replay.Date != testSlotDate {
		t.Fatalf("replayed date = %q, want %q", replay.Date, testSlotDate)
	}
	stored, _, found, err := repo.GetByIdempotencyKey(ctx, "integration-retry")
	if err != nil || !found || stored.Date != testSlotDate {
		t.Fatalf("stored booking = %+v, found = %v, error = %v; want date %q", stored, found, err, testSlotDate)
	}
}

func TestBookingRepository_DeleteBranchServicePreservesBookingHistory(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	booking := newSlotBooking()
	if err := NewBookingRepository(testDB).CreateWithCapacityCheck(ctx, booking); err != nil {
		t.Fatalf("create booking: %v", err)
	}

	if _, err := testDB.ExecContext(ctx, `DELETE FROM branch_services WHERE branch_id = $1 AND service_id = $2`, booking.BranchID, booking.ServiceID); err == nil {
		t.Fatal("expected deleting a referenced branch-service link to be rejected")
	}
	if count := countBookings(t, booking.BranchID, booking.Date, booking.TimeSlot); count != 1 {
		t.Fatalf("booking history count = %d, want 1", count)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_UsesUpdatedBranchCapacity(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)
	setBranchCapacity(t, seededBranchID, 2)

	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); err != nil {
		t.Fatalf("First booking failed: %v", err)
	}
	setBranchCapacity(t, seededBranchID, 1)
	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); !errors.Is(err, domain.ErrCapacityExceeded) {
		t.Errorf("Expected updated capacity to reject the next booking, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_CancelledBookingFreesCapacity(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)
	setBranchCapacity(t, seededBranchID, 1)

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
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)
	setBranchCapacity(t, seededBranchID, 1)

	if err := repo.CreateWithCapacityCheck(ctx, newSlotBooking()); err != nil {
		t.Fatalf("Booking failed: %v", err)
	}

	otherSlot := newSlotBooking()
	otherSlot.TimeSlot = "10:00"
	if err := repo.CreateWithCapacityCheck(ctx, otherSlot); err != nil {
		t.Errorf("A full 09:00 slot must not block 10:00, got %v", err)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_UnknownBranch(t *testing.T) {
	resetDB(t)
	repo := NewBookingRepository(testDB)

	booking := newSlotBooking()
	booking.BranchID = "00000000-0000-0000-0000-000000000000"

	err := repo.CreateWithCapacityCheck(context.Background(), booking)
	if !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected ErrBranchNotFound, got %v", err)
	}
}

func TestBookingRepository_CreateAndQuery(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)

	booking := newSlotBooking()
	if err := repo.CreateWithCapacityCheck(ctx, booking); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	slots, err := repo.GetBookedSlots(ctx, seededBranchID, testSlotDate)
	if err != nil || slots[testSlotTimeSlot] != 1 {
		t.Errorf("GetBookedSlots = %v, %v; want {%s: 1}", slots, err, testSlotTimeSlot)
	}

	byEmail, err := repo.GetByEmailPaginated(ctx, "jane@example.com", 10, 0)
	if err != nil || len(byEmail) != 1 || byEmail[0].ID != booking.ID {
		t.Errorf("GetByEmail = %v, %v; want the created booking", byEmail, err)
	}

	if err := repo.Cancel(ctx, booking.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if err := repo.Cancel(ctx, booking.ID); !errors.Is(err, domain.ErrBookingNotFound) {
		t.Errorf("repeat Cancel = %v, want ErrBookingNotFound", err)
	}
	if slots, _ := repo.GetBookedSlots(ctx, seededBranchID, testSlotDate); slots[testSlotTimeSlot] != 0 {
		t.Errorf("Expected cancelled booking not to be counted, got %d", slots[testSlotTimeSlot])
	}
}

func TestBookingRepository_Cancel_NotFound(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBookingRepository(testDB)
	actions := NewAdminActionRepository(testDB)
	adminID := seededAdminID(t)

	for _, bookingID := range []string{"00000000-0000-0000-0000-000000000000", "abc", "not-a-uuid", "550e8400-e29b-41d4-a716-44665544000z", ""} {
		t.Run(bookingID, func(t *testing.T) {
			if err := repo.Cancel(ctx, bookingID); !errors.Is(err, domain.ErrBookingNotFound) {
				t.Errorf("Cancel: expected ErrBookingNotFound, got %v", err)
			}
			err := actions.CancelBooking(ctx, bookingID, &domain.AuditEntry{
				AdminID: adminID, Action: domain.AuditActionBookingCancel,
				TargetType: domain.AuditTargetBooking, TargetID: bookingID,
			})
			if !errors.Is(err, domain.ErrBookingNotFound) {
				t.Errorf("CancelBooking: expected ErrBookingNotFound, got %v", err)
			}
		})
	}

	var auditCount int
	if err := testDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_log`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("Expected no audit entries for missing bookings, got %d", auditCount)
	}
}
