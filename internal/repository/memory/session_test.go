package memory

import (
	"context"
	"testing"
	"time"

	"github.com/gringottsbankbranchbooking/internal/domain"
)

func TestNewSessionRepository(t *testing.T) {
	repo := NewSessionRepository()

	if repo == nil {
		t.Fatal("Expected repository to be created")
	}

	if repo.sessions == nil {
		t.Fatal("Expected sessions map to be initialized")
	}
}

func TestSessionRepository_Create(t *testing.T) {
	repo := NewSessionRepository()
	ctx := context.Background()

	session := &domain.AdminSession{
		TokenHash: "test-token-123",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	err := repo.Create(ctx, session)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify session was stored
	if _, exists := repo.sessions[session.TokenHash]; !exists {
		t.Error("Expected session to be stored")
	}
}

func TestSessionRepository_GetByTokenHash(t *testing.T) {
	repo := NewSessionRepository()
	ctx := context.Background()

	session := &domain.AdminSession{
		TokenHash: "test-token-456",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Create session first
	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test existing token
	retrieved, err := repo.GetByTokenHash(ctx, "test-token-456")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if retrieved.TokenHash != session.TokenHash {
		t.Errorf("Expected token %s, got %s", session.TokenHash, retrieved.TokenHash)
	}

	if retrieved.AdminID != session.AdminID {
		t.Errorf("Expected admin ID %s, got %s", session.AdminID, retrieved.AdminID)
	}

	// Test non-existing token
	_, err = repo.GetByTokenHash(ctx, "non-existent-token")
	if err == nil {
		t.Error("Expected error for non-existent token")
	}
}

func TestSessionRepository_Delete(t *testing.T) {
	repo := NewSessionRepository()
	ctx := context.Background()

	session := &domain.AdminSession{
		TokenHash: "test-token-789",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Create session first
	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it exists
	if _, exists := repo.sessions[session.TokenHash]; !exists {
		t.Fatal("Expected session to exist before deletion")
	}

	// Delete session
	err := repo.Delete(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify it's deleted
	if _, exists := repo.sessions[session.TokenHash]; exists {
		t.Error("Expected session to be deleted")
	}
}

func TestSessionRepository_CleanupExpired(t *testing.T) {
	repo := NewSessionRepository()
	ctx := context.Background()

	// Create expired session
	expiredSession := &domain.AdminSession{
		TokenHash: "expired-token",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}

	// Create valid session
	validSession := &domain.AdminSession{
		TokenHash: "valid-token",
		AdminID:   "admin1",
		ExpiresAt: time.Now().Add(1 * time.Hour), // Expires in 1 hour
	}

	if err := repo.Create(ctx, expiredSession); err != nil {
		t.Fatalf("Create expired session failed: %v", err)
	}
	if err := repo.Create(ctx, validSession); err != nil {
		t.Fatalf("Create valid session failed: %v", err)
	}

	// Verify both exist
	if len(repo.sessions) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(repo.sessions))
	}

	// Cleanup expired sessions
	err := repo.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify only valid session remains
	if len(repo.sessions) != 1 {
		t.Errorf("Expected 1 session after cleanup, got %d", len(repo.sessions))
	}

	if _, exists := repo.sessions["valid-token"]; !exists {
		t.Error("Expected valid session to remain")
	}

	if _, exists := repo.sessions["expired-token"]; exists {
		t.Error("Expected expired session to be removed")
	}
}
