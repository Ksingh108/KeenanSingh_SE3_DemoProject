package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/GringottsBankBranchBooking/internal/domain"
)

func TestBranchRepository_GetAll(t *testing.T) {
	repo := NewBranchRepository()
	ctx := context.Background()

	branches, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expectedCount := 3
	if len(branches) != expectedCount {
		t.Errorf("Expected %d branches, got %d", expectedCount, len(branches))
	}
}

func TestBranchRepository_GetByID(t *testing.T) {
	repo := NewBranchRepository()
	ctx := context.Background()

	branch, err := repo.GetByID(ctx, "550e8400-e29b-41d4-a716-446655440001")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if branch.Name != "Diagon Alley Main" {
		t.Errorf("Expected 'Diagon Alley Main', got %s", branch.Name)
	}
}

func TestBranchRepository_GetByID_NotFound(t *testing.T) {
	repo := NewBranchRepository()
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent branch")
	}
}

func TestBranchRepository_Delete(t *testing.T) {
	repo := NewBranchRepository()
	ctx := context.Background()
	const seededID = "550e8400-e29b-41d4-a716-446655440001"

	removed, err := repo.Delete(ctx, seededID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if removed != 0 {
		t.Errorf("Expected in-memory delete to report 0 removed bookings, got %d", removed)
	}
	if _, err := repo.GetByID(ctx, seededID); !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected branch to be gone, got err = %v", err)
	}
}

func TestBranchRepository_Delete_NotFound(t *testing.T) {
	repo := NewBranchRepository()

	if _, err := repo.Delete(context.Background(), "does-not-exist"); !errors.Is(err, domain.ErrBranchNotFound) {
		t.Errorf("Expected ErrBranchNotFound, got %v", err)
	}
}
