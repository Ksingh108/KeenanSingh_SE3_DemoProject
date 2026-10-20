package service

import (
	"context"

	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/repository"
)

type BranchService struct {
	branchRepo repository.BranchRepository
}

func NewBranchService(branchRepo repository.BranchRepository) *BranchService {
	return &BranchService{
		branchRepo: branchRepo,
	}
}

func (s *BranchService) GetAllBranches(ctx context.Context) ([]*domain.Branch, error) {
	return s.branchRepo.GetAll(ctx)
}

func (s *BranchService) GetBranchByID(ctx context.Context, id string) (*domain.Branch, error) {
	return s.branchRepo.GetByID(ctx, id)
}
