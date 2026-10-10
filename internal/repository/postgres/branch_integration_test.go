//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

func newTestBranch() *domain.Branch {
	return &domain.Branch{
		Name:     "Integration Branch",
		Address:  "1 Test Street",
		City:     "Cape Town",
		Phone:    "021 555 0100",
		Hours:    "Mon-Fri 9:00-17:00",
		Capacity: 10,
	}
}

func TestBranchRepository_CreateAndGet(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBranchRepository(testDB)

	created, err := repo.Create(ctx, newTestBranch(), []string{seededServiceID})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Errorf("Expected Create to populate ID and CreatedAt, got %+v", created)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "Integration Branch" || got.Capacity != 10 {
		t.Errorf("Unexpected branch: %+v", got)
	}
	if len(got.Services) != 1 || got.Services[0].ID != seededServiceID {
		t.Errorf("Expected the linked service, got %+v", got.Services)
	}
}

// Create runs in a transaction: if linking a service fails, the branch row
// must not be left behind.
func TestBranchRepository_Create_RollsBackOnBadService(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBranchRepository(testDB)

	_, err := repo.Create(ctx, newTestBranch(), []string{"00000000-0000-0000-0000-000000000000"})
	if err == nil {
		t.Fatal("Expected an error for a service that does not exist")
	}

	var count int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM branches WHERE name = 'Integration Branch'`).Scan(&count); err != nil {
		t.Fatalf("count branches: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected the failed create to roll back, found %d leftover branch rows", count)
	}
}

func TestBranchRepository_GetByID_NotFound(t *testing.T) {
	resetDB(t)
	repo := NewBranchRepository(testDB)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected ErrBranchNotFound, got %v", err)
	}
}

// Bookings reference branches with ON DELETE CASCADE, so deleting a branch
// silently removes its bookings. Delete reports how many, for the audit trail.
func TestBranchRepository_Delete_ReportsCascadedBookings(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	branchRepo := NewBranchRepository(testDB)
	bookingRepo := NewBookingRepository(testDB)

	branch, err := branchRepo.Create(ctx, newTestBranch(), []string{seededServiceID})
	if err != nil {
		t.Fatalf("Create branch failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		booking := newSlotBooking()
		booking.BranchID = branch.ID
		if err := bookingRepo.Create(ctx, booking); err != nil {
			t.Fatalf("Create booking failed: %v", err)
		}
	}
	// A booking at another branch must be left alone.
	other := newSlotBooking()
	if err := bookingRepo.Create(ctx, other); err != nil {
		t.Fatalf("Create other booking failed: %v", err)
	}

	removed, err := branchRepo.Delete(ctx, branch.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if removed != 3 {
		t.Errorf("Expected 3 bookings removed, got %d", removed)
	}

	var remaining int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM bookings WHERE branch_id = $1`, branch.ID).Scan(&remaining); err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	if remaining != 0 {
		t.Errorf("Expected the branch's bookings to be gone, found %d", remaining)
	}
	if got := countBookings(t, seededBranchID, testSlotDate, testSlotTimeSlot); got != 1 {
		t.Errorf("Expected the other branch's booking to survive, found %d", got)
	}
	if _, err := branchRepo.GetByID(ctx, branch.ID); !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected the branch to be gone, got %v", err)
	}
}

func TestBranchRepository_Delete_NotFound(t *testing.T) {
	resetDB(t)
	repo := NewBranchRepository(testDB)

	tests := []struct {
		name string
		id   string
	}{
		{"well-formed UUID that does not exist", "00000000-0000-0000-0000-000000000000"},
		// The handler's allow-list accepts this, but Postgres rejects it as a
		// malformed UUID (SQLSTATE 22P02); it must still read as "not found".
		{"identifier that is not a UUID", "does-not-exist"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed, err := repo.Delete(context.Background(), tt.id)
			if !errors.Is(err, domain.ErrBranchNotFound) {
				t.Errorf("Expected ErrBranchNotFound, got %v", err)
			}
			if removed != 0 {
				t.Errorf("Expected 0 removed bookings, got %d", removed)
			}
		})
	}
}
