package memory

import (
	"context"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

func TestAuditRepository_Create(t *testing.T) {
	repo := NewAuditRepository()
	entry := &domain.AuditEntry{
		AdminID:    "admin1",
		Action:     domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking,
		TargetID:   "42",
	}

	if err := repo.Create(context.Background(), entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if entry.ID == "" || entry.CreatedAt.IsZero() {
		t.Errorf("Expected Create to assign ID and CreatedAt, got %+v", entry)
	}
	entries := repo.Entries()
	if len(entries) != 1 || entries[0].TargetID != "42" {
		t.Errorf("Unexpected entries: %+v", entries)
	}
}

func TestAuditRepository_EntriesAreCopies(t *testing.T) {
	repo := NewAuditRepository()
	entry := &domain.AuditEntry{AdminID: "admin1", Action: domain.AuditActionBranchCreate, TargetType: domain.AuditTargetBranch, TargetID: "b1"}
	repo.Create(context.Background(), entry)

	// Mutating the caller's entry or a returned copy must not rewrite history.
	entry.AdminID = "tampered"
	repo.Entries()[0].AdminID = "tampered"

	if got := repo.Entries()[0].AdminID; got != "admin1" {
		t.Errorf("Stored entry was modified through a copy: AdminID = %q", got)
	}
}

func TestAuditRepository_PreservesOrder(t *testing.T) {
	repo := NewAuditRepository()
	for _, id := range []string{"a", "b", "c"} {
		repo.Create(context.Background(), &domain.AuditEntry{AdminID: "admin1", TargetID: id})
	}

	entries := repo.Entries()
	if len(entries) != 3 || entries[0].TargetID != "a" || entries[2].TargetID != "c" {
		t.Errorf("Expected entries in creation order, got %+v", entries)
	}
}
