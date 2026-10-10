package service

import (
	"context"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/GringottsBankBranchBooking/internal/repository"
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

func (s *BranchService) CreateBranch(ctx context.Context, name, city, address, phone string, capacity int, serviceIDs []string) (*domain.Branch, error) {
	branch := &domain.Branch{
		Name:     name,
		City:     city,
		Address:  address,
		Phone:    phone,
		Hours:    "Mon-Fri 9:00-17:00",
		Capacity: capacity,
	}
	return s.branchRepo.Create(ctx, branch, serviceIDs)
}

// DeleteBranch removes a branch and returns how many bookings were removed with
// it, so the caller can record the impact of the deletion.
func (s *BranchService) DeleteBranch(ctx context.Context, id string) (int, error) {
	return s.branchRepo.Delete(ctx, id)
}
