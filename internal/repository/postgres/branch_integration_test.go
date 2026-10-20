//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/domain"
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

func TestBranchRepository_GetAll(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewBranchRepository(testDB)

	withService, err := repo.Create(ctx, &domain.Branch{
		Name: "A Branch With One Service", Address: "1 Test Street", City: "Cape Town",
		Phone: "021 555 0100", Hours: "Mon-Fri 9:00-17:00", Capacity: 10,
	}, []string{seededServiceID})
	if err != nil {
		t.Fatalf("Create branch with service failed: %v", err)
	}
	withoutService, err := repo.Create(ctx, &domain.Branch{
		Name: "Z Branch Without Services", Address: "2 Test Street", City: "Cape Town",
		Phone: "021 555 0101", Hours: "Mon-Fri 9:00-17:00", Capacity: 10,
	}, nil)
	if err != nil {
		t.Fatalf("Create branch without services failed: %v", err)
	}

	branches, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	branchesByID := make(map[string]*domain.Branch, len(branches))
	for index, branch := range branches {
		branchesByID[branch.ID] = branch
		if index > 0 && branches[index-1].Name > branch.Name {
			t.Errorf("Branches are not ordered by name: %q came before %q", branches[index-1].Name, branch.Name)
		}
		for serviceIndex := 1; serviceIndex < len(branch.Services); serviceIndex++ {
			if branch.Services[serviceIndex-1].Name > branch.Services[serviceIndex].Name {
				t.Errorf("Services for branch %q are not ordered by name", branch.Name)
			}
		}
	}

	if got := branchesByID[withService.ID]; got == nil || len(got.Services) != 1 || got.Services[0].ID != seededServiceID {
		t.Errorf("Expected branch %q to have only service %q, got %+v", withService.ID, seededServiceID, got)
	}
	if got := branchesByID[withoutService.ID]; got == nil || len(got.Services) != 0 {
		t.Errorf("Expected branch %q to have no services, got %+v", withoutService.ID, got)
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

// Deleting a branch with live bookings must be rejected so the app cannot
// silently delete customer reservations or audit history.
func TestBranchRepository_Delete_RejectsBranchWithBookings(t *testing.T) {
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
		if err := bookingRepo.CreateWithCapacityCheck(ctx, booking); err != nil {
			t.Fatalf("Create booking failed: %v", err)
		}
	}

	removed, err := branchRepo.Delete(ctx, branch.ID)
	if !errors.Is(err, domain.ErrBranchDeleteBlockedByBookings) {
		t.Fatalf("Expected ErrBranchDeleteBlockedByBookings, got %v", err)
	}
	if removed != 0 {
		t.Errorf("Expected 0 removed bookings, got %d", removed)
	}

	var remaining int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM bookings WHERE branch_id = $1`, branch.ID).Scan(&remaining); err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	if remaining != 3 {
		t.Errorf("Expected the branch's bookings to stay, found %d", remaining)
	}
	if _, err := branchRepo.GetByID(ctx, branch.ID); err != nil {
		t.Errorf("Expected the branch to remain, got %v", err)
	}
}

func TestBranchRepository_Delete_ArchivesBranchAndPreservesCancelledBookings(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	branchRepo := NewBranchRepository(testDB)
	bookingRepo := NewBookingRepository(testDB)

	branch, err := branchRepo.Create(ctx, newTestBranch(), []string{seededServiceID})
	if err != nil {
		t.Fatalf("Create branch failed: %v", err)
	}

	booking := newSlotBooking()
	booking.BranchID = branch.ID
	if err := bookingRepo.CreateWithCapacityCheck(ctx, booking); err != nil {
		t.Fatalf("Create booking failed: %v", err)
	}
	if err := bookingRepo.Cancel(ctx, booking.ID); err != nil {
		t.Fatalf("Cancel booking failed: %v", err)
	}

	preserved, err := branchRepo.Delete(ctx, branch.ID)
	if err != nil {
		t.Fatalf("Delete should archive branch with only cancelled bookings, got %v", err)
	}
	if preserved != 1 {
		t.Errorf("Expected 1 preserved booking, got %d", preserved)
	}
	if _, err := branchRepo.GetByID(ctx, branch.ID); !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected archived branch to be hidden from active lookup, got %v", err)
	}
	var isActive bool
	if err := testDB.QueryRow(`SELECT is_active FROM branches WHERE id = $1`, branch.ID).Scan(&isActive); err != nil {
		t.Fatalf("read archived branch: %v", err)
	}
	if isActive {
		t.Error("Expected archived branch to be inactive")
	}
	var bookingCount int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM bookings WHERE branch_id = $1 AND id = $2`, branch.ID, booking.ID).Scan(&bookingCount); err != nil {
		t.Fatalf("count preserved booking: %v", err)
	}
	if bookingCount != 1 {
		t.Errorf("Expected canceled booking history to remain, found %d rows", bookingCount)
	}
}

func TestBookingRepository_CreateWithCapacityCheck_RejectsArchivedBranch(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	branchRepo := NewBranchRepository(testDB)
	branch, err := branchRepo.Create(ctx, newTestBranch(), []string{seededServiceID})
	if err != nil {
		t.Fatalf("Create branch failed: %v", err)
	}
	if _, err := branchRepo.Delete(ctx, branch.ID); err != nil {
		t.Fatalf("Archive branch failed: %v", err)
	}

	booking := newSlotBooking()
	booking.BranchID = branch.ID
	err = NewBookingRepository(testDB).CreateWithCapacityCheck(ctx, booking)
	if !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected archived branch to reject bookings as not found, got %v", err)
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
