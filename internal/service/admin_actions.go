package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository"
	"github.com/gringottsbankbranchbooking/pkg/logger"
)

// defaultBranchHours is applied to every branch created through the admin API,
// which does not accept opening hours. Slot generation uses a fixed 9:00-17:00
// window regardless of this label, so it is display-only.
const defaultBranchHours = "Mon-Fri 9:00-17:00"

type AdminActionService struct {
	repo repository.AdminActionRepository
}

func NewAdminActionService(repo repository.AdminActionRepository) *AdminActionService {
	return &AdminActionService{repo: repo}
}

func (s *AdminActionService) CancelBooking(ctx context.Context, bookingID, adminID string) error {
	err := s.repo.CancelBooking(ctx, bookingID, &domain.AuditEntry{
		RequestID:  logger.RequestIDFromContext(ctx),
		AdminID:    adminID,
		Action:     domain.AuditActionBookingCancel,
		TargetType: domain.AuditTargetBooking,
		TargetID:   bookingID,
	})
	if err == nil {
		slog.InfoContext(ctx, "admin action", "admin_id", adminID, "action", domain.AuditActionBookingCancel, "target_type", domain.AuditTargetBooking, "target_id", bookingID)
	}
	return err
}

func (s *AdminActionService) CreateBranch(ctx context.Context, name, city, address, phone string, capacity int, serviceIDs []string, adminID string) (*domain.Branch, error) {
	branch := &domain.Branch{
		Name:     name,
		City:     city,
		Address:  address,
		Phone:    phone,
		Hours:    defaultBranchHours,
		Capacity: capacity,
	}
	created, err := s.repo.CreateBranch(ctx, branch, serviceIDs, &domain.AuditEntry{
		RequestID:  logger.RequestIDFromContext(ctx),
		AdminID:    adminID,
		Action:     domain.AuditActionBranchCreate,
		TargetType: domain.AuditTargetBranch,
		Details:    "name=" + name,
	})
	if err == nil {
		slog.InfoContext(ctx, "admin action", "admin_id", adminID, "action", domain.AuditActionBranchCreate, "target_type", domain.AuditTargetBranch, "target_id", created.ID, "details", "name="+name)
	}
	return created, err
}

func (s *AdminActionService) DeleteBranch(ctx context.Context, id, adminID string) (int, error) {
	bookingsPreserved, err := s.repo.DeleteBranch(ctx, id, &domain.AuditEntry{
		RequestID:  logger.RequestIDFromContext(ctx),
		AdminID:    adminID,
		Action:     domain.AuditActionBranchDelete,
		TargetType: domain.AuditTargetBranch,
		TargetID:   id,
	})
	if err != nil {
		return 0, fmt.Errorf("archive branch and record audit: %w", err)
	}
	slog.InfoContext(ctx, "admin action", "admin_id", adminID, "action", domain.AuditActionBranchDelete, "target_type", domain.AuditTargetBranch, "target_id", id, "details", fmt.Sprintf("bookings_preserved=%d", bookingsPreserved))
	return bookingsPreserved, nil
}
