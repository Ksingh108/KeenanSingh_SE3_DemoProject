package service

import (
	"context"
	"testing"

	"github.com/gringottsbankbranchbooking/internal/repository/memory"
)

func TestBranchService_GetAllBranches(t *testing.T) {
	repo := memory.NewBranchRepository()
	service := NewBranchService(repo)

	branches, err := service.GetAllBranches(context.Background())

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(branches) == 0 {
		t.Error("Expected branches, got empty slice")
	}
}

func TestBranchService_GetBranchByID(t *testing.T) {
	repo := memory.NewBranchRepository()
	service := NewBranchService(repo)

	// Get existing branch from memory repo
	branches, _ := service.GetAllBranches(context.Background())
	if len(branches) == 0 {
		t.Skip("No branches available for testing")
	}

	branch, err := service.GetBranchByID(context.Background(), branches[0].ID)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if branch == nil {
		t.Error("Expected branch, got nil")
	}
}
