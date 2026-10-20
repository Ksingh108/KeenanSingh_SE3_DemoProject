package service

import (
	"context"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository/memory"
)

func TestNewCleanupService(t *testing.T) {
	sessionRepo := memory.NewSessionRepository()
	service := NewCleanupService(sessionRepo)

	if service == nil {
		t.Fatal("Expected service to be created")
	}

	if service.sessionRepo == nil {
		t.Fatal("Expected sessionRepo to be set")
	}
}

func TestCleanupService_StartPeriodicCleanup(t *testing.T) {
	sessionRepo := memory.NewSessionRepository()
	service := NewCleanupService(sessionRepo)

	// Create test sessions
	ctx := context.Background()
	expiredSession := &domain.AdminSession{
		TokenHash: "expired-token",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(-2 * time.Hour),
	}

	validSession := &domain.AdminSession{
		TokenHash: "valid-token",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}

	if err := sessionRepo.Create(ctx, expiredSession); err != nil {
		t.Fatalf("Create expired session failed: %v", err)
	}
	if err := sessionRepo.Create(ctx, validSession); err != nil {
		t.Fatalf("Create valid session failed: %v", err)
	}

	// Start cleanup service with a short-lived context
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run cleanup in goroutine
	done := make(chan bool)
	go func() {
		service.StartPeriodicCleanup(cleanupCtx)
		done <- true
	}()

	// Wait for context to be cancelled
	select {
	case <-done:
		// Cleanup service stopped as expected
	case <-time.After(200 * time.Millisecond):
		t.Error("Cleanup service should have stopped when context was cancelled")
	}
}
