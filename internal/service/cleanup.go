package service

import (
	"context"
	"time"

	"github.com/GringottsBankBranchBooking/internal/repository"
	"log/slog"
)

type CleanupService struct {
	sessionRepo repository.SessionRepository
}

func NewCleanupService(sessionRepo repository.SessionRepository) *CleanupService {
	return &CleanupService{
		sessionRepo: sessionRepo,
	}
}

func (s *CleanupService) StartPeriodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sessionRepo.CleanupExpired(ctx); err != nil {
				slog.Error("Operation failed", "operation", "failed to cleanup expired sessions", "error", err.Error())
			} else {
				slog.Info("Cleaned up expired sessions")
			}
		}
	}
}
