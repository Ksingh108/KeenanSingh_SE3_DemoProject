package memory

import (
	"context"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

type AdminActionRepository struct {
	bookings *BookingRepository
	branches *BranchRepository
	audit    *AuditRepository
}

func NewAdminActionRepository(bookings *BookingRepository, branches *BranchRepository, audit *AuditRepository) *AdminActionRepository {
	return &AdminActionRepository{bookings: bookings, branches: branches, audit: audit}
}

func (r *AdminActionRepository) CancelBooking(ctx context.Context, bookingID string, entry *domain.AuditEntry) error {
	if err := r.bookings.Cancel(ctx, bookingID); err != nil {
		return err
	}
	return r.audit.Create(ctx, entry)
}

func (r *AdminActionRepository) CreateBranch(ctx context.Context, branch *domain.Branch, serviceIDs []string, entry *domain.AuditEntry) (*domain.Branch, error) {
	created, err := r.branches.Create(ctx, branch, serviceIDs)
	if err != nil {
		return nil, err
	}
	entry.TargetID = created.ID
	if err := r.audit.Create(ctx, entry); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *AdminActionRepository) DeleteBranch(ctx context.Context, id string, entry *domain.AuditEntry) (int, error) {
	removed, err := r.branches.Delete(ctx, id)
	if err != nil {
		return 0, err
	}
	entry.Details = "bookings_preserved=0"
	if err := r.audit.Create(ctx, entry); err != nil {
		return 0, err
	}
	return removed, nil
}
