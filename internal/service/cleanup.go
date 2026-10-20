package service

import (
	"context"
	"time"

	"log/slog"

	"github.com/gringottsbankbranchbooking/internal/repository"
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
				slog.ErrorContext(ctx, "Operation failed", "operation", "failed to cleanup expired sessions", "error", err.Error())
			} else {
				slog.InfoContext(ctx, "Cleaned up expired sessions")
			}
		}
	}
}
