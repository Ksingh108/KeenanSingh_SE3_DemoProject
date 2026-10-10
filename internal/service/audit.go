package service

import (
	"context"
	"log/slog"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository"
)

// AuditService records privileged admin actions so that every cancel, create
// and delete can be attributed to the admin who performed it.
type AuditService struct {
	auditRepo repository.AuditRepository
}

// NewAuditService creates an AuditService that persists entries via auditRepo.
func NewAuditService(auditRepo repository.AuditRepository) *AuditService {
	return &AuditService{auditRepo: auditRepo}
}

// Record writes an audit entry for an action that has already succeeded.
//
// The structured log line is written first and unconditionally, so the event
// is on record even if the database write then fails. A failed database write
// is logged at ERROR and is deliberately not returned: the action itself has
// already been committed, so reporting failure to the client would mislead
// them. The production hardening is to write the entry in the same database
// transaction as the action, making the two atomic.
func (s *AuditService) Record(ctx context.Context, adminID, action, targetType, targetID, details string) {
	slog.Info("admin action", "operation", "audit", "admin_id", adminID, "action", action, "target_type", targetType, "target_id", targetID, "details", details)

	entry := &domain.AuditEntry{
		AdminID:    adminID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Details:    details,
	}
	if err := s.auditRepo.Create(ctx, entry); err != nil {
		slog.Error("Operation failed", "operation", "record audit entry", "error", err.Error())
	}
}
