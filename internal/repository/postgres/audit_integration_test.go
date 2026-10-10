//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
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
