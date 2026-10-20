//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

func TestAuditRepository_Create(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewAuditRepository(testDB)
	adminID := seededAdminID(t)

	entry := &domain.AuditEntry{
		AdminID:    adminID,
		Action:     domain.AuditActionBranchDelete,
		TargetType: domain.AuditTargetBranch,
		TargetID:   seededBranchID,
		Details:    "bookings_removed=3",
	}
	if err := repo.Create(ctx, entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if entry.ID == "" || entry.CreatedAt.IsZero() {
		t.Errorf("Expected the database to assign ID and CreatedAt, got %+v", entry)
	}

	var admin, action, targetType, targetID, details string
	err := testDB.QueryRow(
		`SELECT admin_id, action, target_type, target_id, details FROM admin_audit_log WHERE id = $1`, entry.ID,
	).Scan(&admin, &action, &targetType, &targetID, &details)
	if err != nil {
		t.Fatalf("read back audit row: %v", err)
	}
	if admin != adminID || action != "branch.delete" || targetType != "branch" || targetID != seededBranchID || details != "bookings_removed=3" {
		t.Errorf("Unexpected stored row: %s %s %s %s %s", admin, action, targetType, targetID, details)
	}
}

func TestAuditRepository_Create_RejectsUnknownAdmin(t *testing.T) {
	resetDB(t)
	repo := NewAuditRepository(testDB)

	err := repo.Create(context.Background(), &domain.AuditEntry{
		AdminID:    "00000000-0000-0000-0000-000000000000",
		Action:     domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking,
		TargetID:   "1",
	})
	if err == nil {
		t.Error("Expected a foreign key error: audit entries must be attributable to a real admin")
	}
}

// Audit history must outlive the admin account: removing an admin who has
// audit entries is refused rather than cascading the history away.
func TestAuditLog_BlocksDeletingAdminWithHistory(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	repo := NewAuditRepository(testDB)
	adminID := seededAdminID(t)

	err := repo.Create(ctx, &domain.AuditEntry{
		AdminID:    adminID,
		Action:     domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking,
		TargetID:   "1",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := testDB.Exec(`DELETE FROM admins WHERE id = $1`, adminID); err == nil {
		t.Error("Expected deleting an admin with audit history to be refused")
	}

	var entries int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM admin_audit_log`).Scan(&entries); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if entries != 1 {
		t.Errorf("Expected the audit history to survive, found %d rows", entries)
	}
}

func TestAdminActionRepository_CancelBooking_RejectsAlreadyCancelled(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	booking := newSlotBooking()
	if err := NewBookingRepository(testDB).CreateWithCapacityCheck(ctx, booking); err != nil {
		t.Fatalf("create booking: %v", err)
	}

	actions := NewAdminActionRepository(testDB)
	adminID := seededAdminID(t)
	for attempt := 0; attempt < 2; attempt++ {
		entry := &domain.AuditEntry{
			AdminID: adminID, Action: domain.AuditActionBookingCancel,
			TargetType: domain.AuditTargetBooking, TargetID: booking.ID,
			RequestID: "2e3152f5-d815-41e2-946c-52964499c430",
		}
		err := actions.CancelBooking(ctx, booking.ID, entry)
		if attempt == 0 && err != nil {
			t.Fatalf("first cancellation: %v", err)
		}
		if attempt == 1 && !errors.Is(err, domain.ErrBookingNotFound) {
			t.Fatalf("repeat cancellation = %v, want ErrBookingNotFound", err)
		}
	}

	var entries int
	if err := testDB.QueryRow(
		`SELECT COUNT(*) FROM admin_audit_log WHERE action = $1 AND target_type = $2 AND target_id = $3`,
		domain.AuditActionBookingCancel, domain.AuditTargetBooking, booking.ID,
	).Scan(&entries); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if entries != 1 {
		t.Errorf("cancellation audit rows = %d, want 1", entries)
	}
	var requestID string
	if err := testDB.QueryRow(`SELECT request_id FROM admin_audit_log WHERE target_id = $1`, booking.ID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if requestID != "2e3152f5-d815-41e2-946c-52964499c430" {
		t.Fatalf("stored request ID = %q", requestID)
	}
}

func TestAdminActionRepository_CancelBooking_RollsBackWhenAuditFails(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	booking := newSlotBooking()
	if err := NewBookingRepository(testDB).CreateWithCapacityCheck(ctx, booking); err != nil {
		t.Fatalf("create booking: %v", err)
	}

	actions := NewAdminActionRepository(testDB)
	err := actions.CancelBooking(ctx, booking.ID, &domain.AuditEntry{
		AdminID: "00000000-0000-0000-0000-000000000000", Action: domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking, TargetID: booking.ID,
	})
	if err == nil {
		t.Fatal("expected audit foreign-key failure")
	}

	var status string
	if err := testDB.QueryRow(`SELECT status FROM bookings WHERE id = $1`, booking.ID).Scan(&status); err != nil {
		t.Fatalf("read booking status: %v", err)
	}
	if status != string(domain.StatusConfirmed) {
		t.Errorf("booking status = %q, want confirmed after rollback", status)
	}
}

func TestAdminActionRepository_CreateBranch_RollsBackWhenAuditFails(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	branch := newTestBranch()
	branch.Name = "Atomic Audit Branch"

	_, err := NewAdminActionRepository(testDB).CreateBranch(ctx, branch, []string{seededServiceID}, &domain.AuditEntry{
		AdminID: "00000000-0000-0000-0000-000000000000", Action: domain.AuditActionBranchCreate,
		TargetType: domain.AuditTargetBranch, Details: "name=" + branch.Name,
	})
	if err == nil {
		t.Fatal("expected audit foreign-key failure")
	}

	var count int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM branches WHERE name = $1`, branch.Name).Scan(&count); err != nil {
		t.Fatalf("count branches: %v", err)
	}
	if count != 0 {
		t.Errorf("found %d branch rows after failed audit; want rollback", count)
	}
}

func TestAdminActionRepository_DeleteBranch_RollsBackWhenAuditFails(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	branch, err := NewBranchRepository(testDB).Create(ctx, newTestBranch(), []string{seededServiceID})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	_, err = NewAdminActionRepository(testDB).DeleteBranch(ctx, branch.ID, &domain.AuditEntry{
		AdminID: "00000000-0000-0000-0000-000000000000", Action: domain.AuditActionBranchDelete,
		TargetType: domain.AuditTargetBranch, TargetID: branch.ID,
	})
	if err == nil {
		t.Fatal("expected audit foreign-key failure")
	}

	if _, err := NewBranchRepository(testDB).GetByID(ctx, branch.ID); err != nil {
		t.Errorf("branch should remain after failed audit: %v", err)
	}
}
