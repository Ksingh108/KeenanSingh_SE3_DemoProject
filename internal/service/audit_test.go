package service

import (
	"context"
	"errors"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository/memory"
)

type failingAuditRepository struct{}

func (failingAuditRepository) Create(context.Context, *domain.AuditEntry) error {
	return errors.New("database unavailable")
}

func TestAuditService_Record_PersistsEntry(t *testing.T) {
	repo := memory.NewAuditRepository()
	service := NewAuditService(repo)

	service.Record(context.Background(), "admin1", domain.AuditActionBranchDelete, domain.AuditTargetBranch, "branch-1", "bookings_removed=2")

	entries := repo.Entries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.AdminID != "admin1" || got.Action != domain.AuditActionBranchDelete ||
		got.TargetType != domain.AuditTargetBranch || got.TargetID != "branch-1" || got.Details != "bookings_removed=2" {
		t.Errorf("Unexpected entry: %+v", got)
	}
}

// The action has already been committed when Record is called, so a failing
// audit store must not panic or surface to the caller.
func TestAuditService_Record_SurvivesRepositoryFailure(t *testing.T) {
	service := NewAuditService(failingAuditRepository{})

	service.Record(context.Background(), "admin1", domain.AuditActionBookingCancel, domain.AuditTargetBooking, "1", "")
}
