package memory

import (
	"context"
	"testing"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

func TestNewAdminRepository(t *testing.T) {
	repo := NewAdminRepository()

	if repo == nil {
		t.Fatal("Expected repository to be created")
	}

	if repo.admins == nil {
		t.Fatal("Expected admins map to be initialized")
	}

	// Check seeded data
	admin, exists := repo.admins["admin"]
	if !exists {
		t.Fatal("Expected seeded admin user to exist")
	}

	if admin.Username != "admin" {
		t.Errorf("Expected username 'admin', got %s", admin.Username)
	}
}

func TestAdminRepository_GetByUsername(t *testing.T) {
	repo := NewAdminRepository()

	// Test existing user
	admin, err := repo.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if admin.Username != "admin" {
		t.Errorf("Expected username 'admin', got %s", admin.Username)
	}

	// Verify password hash can be used
	err = bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("admin123"))
	if err != nil {
		t.Error("Expected password hash to match 'admin123'")
	}

	// Test non-existing user
	_, err = repo.GetByUsername(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestAdminRepository_Create(t *testing.T) {
	repo := NewAdminRepository()

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
	newAdmin := &domain.Admin{
		ID:           "test1",
		Username:     "testuser",
		PasswordHash: string(hashedPassword),
		CreatedAt:    time.Now(),
	}

	err := repo.Create(newAdmin)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify admin was created
	admin, err := repo.GetByUsername(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("Expected to find created admin, got error: %v", err)
	}

	if admin.ID != "test1" {
		t.Errorf("Expected ID 'test1', got %s", admin.ID)
	}
}
